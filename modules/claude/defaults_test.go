package claude

import (
	"strings"
	"testing"
)

// Every session gets what was settled for all of them.
//
// A shell alias cannot reach here: it is expanded by an interactive shell
// reading a command line, and this application executes the program. So
// `--effort max`, which an alias adds to every claude typed in a terminal,
// reached none of the sessions opened here.
func TestASessionStartsWithTheSharedArguments(t *testing.T) {
	t.Cleanup(func() { SetDefaults("", nil) })
	SetDefaults("", []string{"--effort", "max"})

	built, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m := built.(*Module)
	got := strings.Join(Argv(m.binary, "an-id", "", m.extra), " ")
	if want := "claude --session-id an-id --effort max"; got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// A pane's own arguments come after the shared ones, so what it repeats is
// what counts — which is how a flag given twice is read.
func TestAPanesOwnArgumentsHaveTheLastWord(t *testing.T) {
	t.Cleanup(func() { SetDefaults("", nil) })
	SetDefaults("", []string{"--effort", "max"})

	built, err := New(map[string]any{"args": []any{"--effort", "low"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m := built.(*Module)
	got := strings.Join(m.extra, " ")
	if want := "--effort max --effort low"; got != want {
		t.Errorf("arguments = %q, want %q", got, want)
	}
}

// And the program itself can be settled the same way, for a claude that is not
// first on the path.
func TestTheProgramCanBeSettledForEverySession(t *testing.T) {
	t.Cleanup(func() { SetDefaults("", nil) })
	SetDefaults("/opt/claude/bin/claude", nil)

	built, _ := New(nil)
	if got := built.(*Module).binary; got != "/opt/claude/bin/claude" {
		t.Errorf("binary = %q", got)
	}
	// What a pane names itself still wins.
	built, _ = New(map[string]any{"bin": "/usr/bin/claude"})
	if got := built.(*Module).binary; got != "/usr/bin/claude" {
		t.Errorf("a pane could not name its own program: %q", got)
	}
}
