package pool_test

import (
	"syscall"
	"testing"
	"time"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
)

func start(t *testing.T, id string) *session.Session {
	t.Helper()
	s, err := session.Start(session.Spec{
		ID: session.ID(id), Argv: []string{"sleep", "30"},
		Dir: t.TempDir(), Width: 20, Height: 5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// Detaching must not stop anything. That is the whole point of the pool: a
// session with no pane on screen is still a session.
func TestDetachingLeavesTheSessionRunning(t *testing.T) {
	p := pool.New(bus.New())
	defer p.CloseAll()

	s := start(t, "a")
	p.Add(s, "portal", "/tmp")
	p.SetAttached("a", true)
	p.SetAttached("a", false)

	e, ok := p.Get("a")
	if !ok {
		t.Fatal("Get(a) found nothing")
	}
	if e.Attached {
		t.Error("the entry is still marked attached")
	}
	if st, _ := s.Status(); st != session.Running {
		t.Error("detaching stopped the process")
	}
}

func TestKillEndsTheProcessAndForgetsIt(t *testing.T) {
	p := pool.New(bus.New())
	defer p.CloseAll()

	s := start(t, "b")
	p.Add(s, "api", "/tmp")
	if err := p.Kill("b"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, ok := p.Get("b"); ok {
		t.Error("the entry survived Kill")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := s.Status(); st == session.Exited {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the process is still running after Kill")
}

func TestWaitingCountsOnlySessionsThatNeedYou(t *testing.T) {
	p := pool.New(bus.New())
	defer p.CloseAll()

	p.Add(start(t, "c1"), "one", "/tmp")
	p.Add(start(t, "c2"), "two", "/tmp")
	p.Add(start(t, "c3"), "three", "/tmp")
	p.SetState("c1", pool.StateWorking)
	p.SetState("c2", pool.StateWaiting)
	p.SetState("c3", pool.StateWaiting)

	if got := p.Waiting(); got != 2 {
		t.Fatalf("Waiting() = %d, want 2", got)
	}
}

// Every change is announced, so the list redraws without polling.
func TestChangesArePublished(t *testing.T) {
	b := bus.New()
	defer b.Close()
	ch := b.SubscribeState("session.state")
	p := pool.New(b)
	defer p.CloseAll()

	p.Add(start(t, "d"), "portal", "/tmp")
	select {
	case v := <-ch:
		entries, ok := v.([]*pool.Entry)
		if !ok {
			t.Fatalf("published %T, want []*pool.Entry", v)
		}
		if len(entries) != 1 || entries[0].Title != "portal" {
			t.Fatalf("published %+v, want one entry titled portal", entries)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("adding a session published nothing")
	}
}

// All returns copies. A caller must not be able to reach in and change the
// pool's own bookkeeping by writing to what it was handed.
func TestAllHandsOutCopies(t *testing.T) {
	p := pool.New(bus.New())
	defer p.CloseAll()
	p.Add(start(t, "e"), "portal", "/tmp")

	all := p.All()
	all[0].Title = "tampered"

	e, _ := p.Get("e")
	if e.Title != "portal" {
		t.Fatalf("the pool's entry became %q; All handed out its own state", e.Title)
	}
}

// A session whose process is gone is not waiting for you, whatever the last
// hook said about it.
//
// The hooks are reports about a process; the process is the fact. A
// conversation that ended while it was waiting — killed, crashed, or simply
// closed without its last hook arriving — kept the word "waiting" for as long
// as the application was open. The title said "2 waiting" over a single
// running session, and the key that goes to what is waiting had nowhere to go.
func TestASessionWhoseProcessIsGoneIsNotWaiting(t *testing.T) {
	p := pool.New(bus.New())
	s, err := session.Start(session.Spec{
		ID: "goner", Argv: []string{"sh", "-c", "sleep 30"}, Dir: ".",
		Width: 20, Height: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	p.Add(s, "goner", ".")
	p.SetState(s.ID, pool.StateWaiting)
	if got := p.Waiting(); got != 1 {
		t.Fatalf("Waiting = %d while it really was waiting", got)
	}

	// The process ends without anything telling the pool.
	if err := syscall.Kill(s.Pid(), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := s.Status(); st == session.Exited {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if got := p.Waiting(); got != 0 {
		t.Errorf("Waiting = %d after the process died", got)
	}
	all := p.All()
	if len(all) != 1 {
		t.Fatalf("%d entries, want the session still listed", len(all))
	}
	if all[0].State != pool.StateExited {
		t.Errorf("state = %v, want exited", all[0].State)
	}
}

// A conversation whose pane was closed keeps running — that is what closing a
// tab does to a Claude session — and it is not what the window is waiting on.
// Counted, it said "5 waiting" to somebody looking at two.
func TestWaitingIgnoresAConversationNoPaneIsShowing(t *testing.T) {
	p := pool.New(bus.New())
	defer p.CloseAll()

	p.Add(start(t, "shown"), "shown", "/tmp")
	p.Add(start(t, "detached"), "detached", "/tmp")
	p.SetState("shown", pool.StateWaiting)
	p.SetState("detached", pool.StateWaiting)
	p.SetAttached("detached", false)

	if got := p.Waiting(); got != 1 {
		t.Errorf("Waiting() = %d, want 1 — the one a pane is showing", got)
	}
}
