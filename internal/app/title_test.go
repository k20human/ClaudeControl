package app

import (
	"strings"
	"testing"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
)

// titlePool is a pool holding one session per state asked for.
func titlePool(t *testing.T, states ...pool.State) *pool.Pool {
	t.Helper()
	p := pool.New(bus.New())
	for i, st := range states {
		s, err := session.Start(session.Spec{
			ID:    session.ID(string(rune('a' + i))),
			Argv:  []string{"sh", "-c", "sleep 30"},
			Dir:   ".",
			Width: 20, Height: 5,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		p.Add(s, "one", ".")
		p.SetState(s.ID, st)
	}
	return p
}

// The title is what you can see when the window is behind another, which is
// exactly when a question would otherwise go unnoticed. So it reports what
// wants something from you.
func TestTheTitleReportsWhatWantsYou(t *testing.T) {
	a := newTestApp(t)
	a.pool = titlePool(t, pool.StateWaiting, pool.StateWorking)

	got := a.windowTitle()
	if !strings.Contains(got, "waiting") {
		t.Errorf("title = %q, want the session that is waiting", got)
	}
}

// A session that has ended asks nothing. It is drawn as ended above its pane
// and in its tab, where saying so is the point — but a title that read
// "1 exited" until you closed the pane would be a permanent notice about
// something that needs no doing.
func TestTheTitleIgnoresASessionThatHasEnded(t *testing.T) {
	a := newTestApp(t)
	a.pool = titlePool(t, pool.StateExited, pool.StateWorking)

	got := a.windowTitle()
	if strings.Contains(got, "exited") {
		t.Errorf("title = %q, want no word about a session that ended", got)
	}
	if !strings.Contains(got, "working") {
		t.Errorf("title = %q, want the session that is working", got)
	}
}

// And with nothing but ended sessions, the title is just the name.
func TestTheTitleIsTheNameWhenNothingWantsYou(t *testing.T) {
	a := newTestApp(t)
	a.pool = titlePool(t, pool.StateExited, pool.StateIdle)

	if got := a.windowTitle(); got != "ClaudeControl" {
		t.Errorf("title = %q, want just the name", got)
	}
}
