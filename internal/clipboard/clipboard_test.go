package clipboard_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/clipboard"
)

// A missing helper is its own error, because its remedy is its own: one
// command to install one, rather than a failure to investigate.
func TestNoHelperIsItsOwnError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, ok := clipboard.Helper(); ok {
		t.Fatal("a helper was found on an empty PATH")
	}
	if _, err := clipboard.Read(time.Second); !errors.Is(err, clipboard.ErrNoHelper) {
		t.Errorf("Read = %v, want ErrNoHelper", err)
	}
}

func TestReadUsesTheHelperItFound(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "wl-paste")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'from the clipboard\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	if name, ok := clipboard.Helper(); !ok || name != "wl-paste" {
		t.Fatalf("Helper = %q, %v", name, ok)
	}
	got, err := clipboard.Read(2 * time.Second)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The trailing newline goes: pasting it would submit the line in anything
	// that reads a line at a time.
	if got != "from the clipboard" {
		t.Errorf("Read = %q", got)
	}
}

// A helper whose clipboard owner has gone away can wait for ever, and an
// interface must not wait with it.
func TestReadGivesUp(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "wl-paste")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	start := time.Now()
	if _, err := clipboard.Read(200 * time.Millisecond); err == nil {
		t.Fatal("a hanging helper was accepted")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("gave up after %v", d)
	}
}

// The two selections are two places, and the flags are what tells the helper
// which one is meant. A primary write that reached the clipboard would cost
// you what you had copied, every time you dragged across a line.
func TestEachSelectionIsAskedForByName(t *testing.T) {
	for _, c := range []struct {
		sel  clipboard.Selection
		want string
	}{
		{clipboard.System, "wl-copy"},
		{clipboard.Primary, "wl-copy --primary"},
	} {
		dir := t.TempDir()
		got := filepath.Join(dir, "argv")
		script := "#!/bin/sh\nprintf 'wl-copy %s' \"$*\" > " + got + "\ncat > /dev/null\n"
		if err := os.WriteFile(filepath.Join(dir, "wl-copy"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		// First on the path, not alone on it: the stand-in is a shell script
		// and needs the shell's own tools behind it.
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		if err := clipboard.WriteTo(c.sel, "text", time.Second); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		b, err := os.ReadFile(got)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(b)) != c.want {
			t.Errorf("ran %q, want %q", strings.TrimSpace(string(b)), c.want)
		}
	}
}

// And the same for reading, where the wrong flag would paste what you copied
// instead of what you selected.
func TestReadingNamesTheSelectionToo(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = --primary ]; then printf 'selected'; else printf 'copied'; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "wl-paste"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got, err := clipboard.ReadFrom(clipboard.Primary, time.Second); err != nil || got != "selected" {
		t.Errorf("ReadFrom(Primary) = %q, %v", got, err)
	}
	if got, err := clipboard.ReadFrom(clipboard.System, time.Second); err != nil || got != "copied" {
		t.Errorf("ReadFrom(System) = %q, %v", got, err)
	}
}
