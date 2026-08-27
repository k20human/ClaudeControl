package transcript_test

import (
	"os"
	"path/filepath"
	"testing"

	"claudecontrol/internal/transcript"
)

// A session that never said anything has no transcript, and resuming it fails
// with an error there is nothing useful to do about. Knowing beforehand is
// what lets the pane start fresh instead.
func TestFindLooksUnderEveryProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	projects := filepath.Join(dir, "projects")
	for _, p := range []string{"-home-k-api", "-home-k-portal"} {
		if err := os.MkdirAll(filepath.Join(projects, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := filepath.Join(projects, "-home-k-portal", "abc-123.jsonl")
	if err := os.WriteFile(want, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, ok := transcript.Find("abc-123")
	if !ok || got != want {
		t.Errorf("Find = %q, %v; want %q", got, ok, want)
	}
	if !transcript.Exists("abc-123") {
		t.Error("Exists said no about a transcript that is there")
	}

	if _, ok := transcript.Find("never-spoke"); ok {
		t.Error("Find invented a transcript")
	}
	if transcript.Exists("") {
		t.Error("Exists said yes about no session at all")
	}
}

// A missing projects directory is a first run, not a failure.
func TestFindOnAMachineWithNoProjectsYet(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if transcript.Exists("anything") {
		t.Error("Exists said yes with no projects directory at all")
	}
}
