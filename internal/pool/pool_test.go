package pool_test

import (
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
