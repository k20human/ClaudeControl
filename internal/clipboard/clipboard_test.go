package clipboard_test

import (
	"errors"
	"os"
	"path/filepath"
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
