package transcript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/transcript"
)

// write lays out a transcript the way Claude Code does.
func writeTranscript(t *testing.T, root, project, id string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, "projects", project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Only what was said. A word that is common in code would otherwise match
// every conversation that ever opened a source file, which is the opposite of
// finding something.
func TestSearchLooksOnlyAtWhatWasSaid(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)

	writeTranscript(t, root, "-home-k-api", "spoken",
		`{"type":"ai-title","aiTitle":"The hologram panel"}`,
		`{"type":"user","cwd":"/home/k/api","message":{"content":"can we make the HOLOGRAM turn?"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"The hologram already turns."}]}}`,
	)
	writeTranscript(t, root, "-home-k-web", "machinery",
		`{"type":"user","cwd":"/home/k/web","message":{"content":[{"type":"tool_result","content":"hologram.go compiled"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"grep hologram ."}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"the hologram is fine"}]}}`,
	)

	got := transcript.Search("hologram", 0)
	if len(got) != 1 {
		t.Fatalf("Search found %d conversations, want the one where it was said: %+v", len(got), got)
	}
	h := got[0]
	if h.SessionID != "spoken" {
		t.Errorf("SessionID = %q", h.SessionID)
	}
	if h.Title != "The hologram panel" {
		t.Errorf("Title = %q", h.Title)
	}
	if h.Dir != "/home/k/api" {
		t.Errorf("Dir = %q", h.Dir)
	}
	// Both the question and the answer.
	if h.Matches != 2 {
		t.Errorf("Matches = %d, want 2", h.Matches)
	}
	if !strings.Contains(strings.ToLower(h.Excerpt), "hologram") {
		t.Errorf("Excerpt = %q", h.Excerpt)
	}
}

// Case is ignored, because you are looking for something half remembered.
func TestSearchIgnoresCase(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	writeTranscript(t, root, "-p", "s",
		`{"type":"user","message":{"content":"The Supervisor keeps NPM running"}}`)

	for _, q := range []string{"supervisor", "SUPERVISOR", "SuPerVisor"} {
		if got := transcript.Search(q, 0); len(got) != 1 {
			t.Errorf("Search(%q) found %d", q, len(got))
		}
	}
}

// Newest first: you are usually looking for something recent, and ordering by
// how often a word appears would put a long-ago conversation that repeated it
// above the one you had this morning.
func TestSearchPutsTheNewestFirst(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	old := writeTranscript(t, root, "-p", "older",
		`{"type":"user","message":{"content":"needle needle needle"}}`)
	recent := writeTranscript(t, root, "-p", "newer",
		`{"type":"user","message":{"content":"needle"}}`)

	past := mustTime(t, "2020-01-01T00:00:00Z")
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	_ = recent

	got := transcript.Search("needle", 0)
	if len(got) != 2 {
		t.Fatalf("Search found %d", len(got))
	}
	if got[0].SessionID != "newer" {
		t.Errorf("the newest is %q, want newer", got[0].SessionID)
	}
	// And the one with more of them is still there, just second.
	if got[1].Matches != 1 {
		t.Errorf("the older conversation reports %d matches", got[1].Matches)
	}
}

// An empty query finds nothing rather than everything.
func TestAnEmptyQueryFindsNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	writeTranscript(t, root, "-p", "s", `{"type":"user","message":{"content":"anything"}}`)

	for _, q := range []string{"", "   "} {
		if got := transcript.Search(q, 0); len(got) != 0 {
			t.Errorf("Search(%q) found %d", q, len(got))
		}
	}
}

// A machine with no transcripts is a first run, not a failure.
func TestSearchOnAnEmptyMachine(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if got := transcript.Search("anything", 0); len(got) != 0 {
		t.Errorf("Search found %d on a machine with nothing", len(got))
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
