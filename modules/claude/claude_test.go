package claude_test

import (
	"strings"
	"testing"

	"claudecontrol/modules/claude"
)

// Without --session-id we could not tell which transcript, and which hook,
// belongs to which pane: two sessions in one directory are indistinguishable.
func TestArgvImposesTheSessionIdentity(t *testing.T) {
	got := claude.Argv("claude", "3f2a-uuid", "", nil)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--session-id 3f2a-uuid") {
		t.Fatalf("argv = %v, want it to impose the session id", got)
	}
	if strings.Contains(joined, "--resume") {
		t.Errorf("argv = %v, want no --resume for a fresh session", got)
	}
}

func TestArgvResumesWhenAskedTo(t *testing.T) {
	got := claude.Argv("claude", "new-id", "old-id", nil)
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--resume old-id") {
		t.Fatalf("argv = %v, want it to resume the previous session", got)
	}
	// Resuming adopts the old identity; imposing a new one at the same time
	// would ask Claude Code for two different things at once.
	if strings.Contains(joined, "--session-id") {
		t.Errorf("argv = %v, want no --session-id alongside --resume", got)
	}
}

func TestArgvKeepsUserArguments(t *testing.T) {
	got := claude.Argv("claude", "id", "", []string{"--effort", "max"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--effort max") {
		t.Fatalf("argv = %v, want the extra arguments preserved", got)
	}
}
