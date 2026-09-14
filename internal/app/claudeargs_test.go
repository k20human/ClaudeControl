package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// recordingClaude is a stand-in that writes down how it was called.
func recordingClaude(t *testing.T) (onPath, wrote string) {
	t.Helper()
	dir := t.TempDir()
	wrote = filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + wrote + "\nprintf 'FAKE-CLAUDE\\n'\ncat\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, wrote
}

// A session opened at runtime starts with the arguments settled for all of
// them.
//
// This is the path a shell alias can never reach. `alias claude='claude
// --effort max'` is expanded by an interactive shell reading a command line;
// alt+a executes the program. The flag reached every session typed in a
// terminal and none of the sessions opened here.
func TestASessionOpenedWithAltAGetsTheSettledArguments(t *testing.T) {
	const W, H = 100, 14
	work := filepath.Join(t.TempDir(), "DEV")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	onPath, wrote := recordingClaude(t)

	cfg := filepath.Join(t.TempDir(), "settled.yaml")
	body := `claude:
  args: ["--effort", "max"]
layout:
  module: tabs
  options:
    dir: ` + work + `
    tabs:
      - title: shell
        module: term
        options: { cmd: [sh, -c, "printf SHELL-HERE; cat"] }
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	s, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, []string{
		"PATH=" + onPath + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CLAUDE_CONFIG_DIR=" + t.TempDir(),
	})
	waitForAnywhere(t, snap, "SHELL-HERE")

	s.SendText("\x1ba") // alt+a
	waitForAnywhere(t, snap, "FAKE-CLAUDE")

	got := waitForArgv(t, wrote)
	if !strings.Contains(got, "--effort max") {
		t.Errorf("the session was started as %q, want the settled arguments in it", got)
	}
	// And still identified: the arguments are added to what makes a session
	// findable, not put in its place.
	if !strings.Contains(got, "--session-id") {
		t.Errorf("the session lost its identity: %q", got)
	}
}

func waitForArgv(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b))
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the stand-in was never called")
	return ""
}
