package transcript_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claudecontrol/internal/transcript"
)

// place writes a transcript naming a working directory, and dates it.
func place(t *testing.T, root, project, id, cwd string, at time.Time) {
	t.Helper()
	line := `{"type":"user","cwd":"` + cwd + `","message":{"content":"hello"}}` + "\n"
	path := writeTranscript(t, root, project, id, line[:len(line)-1])
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// The directories you have worked in, most recent first — which is the order
// you want them offered in, because the one you want is nearly always one of
// the last few.
func TestPlacesAreTheDirectoriesYouHaveWorkedIn(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	older := filepath.Join(root, "older")
	newer := filepath.Join(root, "newer")
	for _, d := range []string{older, newer} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now()
	place(t, root, "-older", "aaaa", older, now.Add(-2*time.Hour))
	place(t, root, "-newer", "bbbb", newer, now)

	got := transcript.Places(10)
	if len(got) != 2 {
		t.Fatalf("%d places, want 2: %+v", len(got), got)
	}
	if got[0].Dir != newer {
		t.Errorf("first place is %q, want the one used most recently", got[0].Dir)
	}
	if got[1].Dir != older {
		t.Errorf("second place is %q", got[1].Dir)
	}
}

// A directory that has been deleted is not somewhere to open anything.
func TestAPlaceThatNoLongerExistsIsNotOffered(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	gone := filepath.Join(root, "gone")
	place(t, root, "-gone", "aaaa", gone, time.Now())

	if got := transcript.Places(10); len(got) != 0 {
		t.Errorf("a deleted directory is still offered: %+v", got)
	}
}

// One directory, however many conversations were had in it, and dated by the
// most recent of them.
func TestOneDirectoryIsOfferedOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	dir := filepath.Join(root, "twice")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	place(t, root, "-twice-a", "aaaa", dir, now.Add(-3*time.Hour))
	place(t, root, "-twice-b", "bbbb", dir, now)

	got := transcript.Places(10)
	if len(got) != 1 {
		t.Fatalf("%d places for one directory: %+v", len(got), got)
	}
	if got[0].Last.Before(now.Add(-time.Minute)) {
		t.Errorf("dated %v, want the most recent conversation there", got[0].Last)
	}
}

// The limit is a limit.
func TestPlacesStopAtTheLimit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	now := time.Now()
	for _, name := range []string{"one", "two", "three"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		place(t, root, "-"+name, "id-"+name, dir, now)
	}
	if got := transcript.Places(2); len(got) != 2 {
		t.Errorf("%d places, want the 2 asked for", len(got))
	}
}
