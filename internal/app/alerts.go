package app

import (
	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
)

// bellSink is where the notifier's bell is written, and it is not the
// terminal.
//
// The bell is raised on the hook pump's goroutine while the drawing loop is
// writing frames to that same terminal, and a byte landing in the middle of an
// escape sequence would corrupt the screen. So the bell is recorded here and
// rung by the drawing loop, which is the one writer the terminal has.
type bellSink struct{ a *App }

func (b bellSink) Write(p []byte) (int, error) {
	b.a.bellPending.Store(true)
	b.a.Wake()
	return len(p), nil
}

// ringPendingBell writes the bell if one was raised since the last frame. Ends
// a frame rather than starting one: the notice arrives with the screen that
// shows what it is about.
func (a *App) ringPendingBell() {
	if a.bellPending.Swap(false) && a.term != nil {
		_, _ = a.term.Write([]byte("\a"))
	}
}

// noteAlert says once that a conversation has started waiting on you.
//
// Once, and on the way in only: hooks arrive whenever Claude Code has
// something to report, and a bell on each of them would be an alarm rather
// than a notice. What is announced is the transition — the moment the answer
// stopped being Claude's and became yours.
//
// Runs on the hook pump's goroutine, so it touches nothing the drawing loop
// owns: the notifier has its own lock and waitingMark belongs to this pump.
func (a *App) noteAlert(key string, st pool.State) {
	if a.alerts == nil || key == "" {
		return
	}
	now := st == pool.StateWaiting
	was := a.waitingMark[key]
	a.waitingMark[key] = now
	if !now || was {
		return
	}
	_ = a.alerts.Waiting("Claude is waiting", a.alertBody(key))
}

// alertBody names the conversation, so a screen of them tells you which.
func (a *App) alertBody(key string) string {
	if a.pool != nil {
		if e, ok := a.pool.Get(session.ID(key)); ok && e.Title != "" {
			return e.Title + " is waiting on you"
		}
	}
	return "a conversation is waiting on you"
}
