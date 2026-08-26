// Package pool owns the running sessions. They live here rather than in the
// layout tree, so a session with no pane on screen keeps running.
package pool

// State is how a session appears in the sessions list.
type State int

const (
	// StateIdle means the turn is over and Claude is waiting for a prompt.
	StateIdle State = iota
	// StateWorking means Claude is generating or running a tool.
	StateWorking
	// StateWaiting means Claude needs an answer from you — a permission
	// prompt, or a notification. This is the one worth spotting across a
	// screenful of panes.
	StateWaiting
	// StateExited means the process is gone.
	StateExited
)

func (s State) String() string {
	switch s {
	case StateWorking:
		return "working"
	case StateWaiting:
		return "waiting"
	case StateExited:
		return "exited"
	default:
		return "idle"
	}
}

// hookStates is the mapping of spec section 4.5. It is deliberately exhaustive
// rather than defaulting: an event nobody planned for must not move a session
// into a state nobody expects.
var hookStates = map[string]State{
	"UserPromptSubmit": StateWorking,
	"PreToolUse":       StateWorking,
	"PostToolUse":      StateWorking,
	"Notification":     StateWaiting,
	"Stop":             StateIdle,
	"SessionEnd":       StateExited,
}

// StateForHook returns the state a hook event implies, and whether the event is
// one we act on at all.
func StateForHook(event string) (State, bool) {
	s, ok := hookStates[event]
	return s, ok
}
