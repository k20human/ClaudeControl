package pool_test

import (
	"testing"

	"claudecontrol/internal/pool"
)

func TestStateForHookMapsEveryEventWeSubscribeTo(t *testing.T) {
	cases := []struct {
		event string
		want  pool.State
	}{
		{"UserPromptSubmit", pool.StateWorking},
		{"PreToolUse", pool.StateWorking},
		{"PostToolUse", pool.StateWorking},
		{"Notification", pool.StateWaiting},
		{"Stop", pool.StateIdle},
		{"SessionEnd", pool.StateExited},
	}
	for _, c := range cases {
		got, ok := pool.StateForHook(c.event)
		if !ok {
			t.Errorf("StateForHook(%q) reported no mapping", c.event)
			continue
		}
		if got != c.want {
			t.Errorf("StateForHook(%q) = %v, want %v", c.event, got, c.want)
		}
	}
}

// An event we do not subscribe to must be reported as unmapped rather than
// silently folded into a state, or a future hook would move sessions about for
// reasons nobody asked for.
func TestStateForHookRejectsAnythingElse(t *testing.T) {
	for _, event := range []string{"SubagentStop", "PreCompact", "SessionStart", ""} {
		if _, ok := pool.StateForHook(event); ok {
			t.Errorf("StateForHook(%q) claimed a mapping", event)
		}
	}
}

func TestStateStringsAreStable(t *testing.T) {
	cases := map[pool.State]string{
		pool.StateIdle:    "idle",
		pool.StateWorking: "working",
		pool.StateWaiting: "waiting",
		pool.StateExited:  "exited",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", s, got, want)
		}
	}
}
