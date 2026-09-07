package bus_test

import (
	"sync"
	"testing"
	"time"

	"claudecontrol/internal/bus"
)

func recv(t *testing.T, ch <-chan any, what string) any {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return nil
	}
}

// A state topic keeps only the latest value. A subscriber that looks away for
// a moment must come back to the present, not to a backlog it will never care
// about.
func TestStateTopicKeepsOnlyTheLatestValue(t *testing.T) {
	b := bus.New()
	defer b.Close()
	ch := b.SubscribeState("session.state")

	for i := 1; i <= 100; i++ {
		b.PublishState("session.state", i)
	}
	if got := recv(t, ch, "the latest state"); got != 100 {
		t.Fatalf("received %v, want the newest value 100", got)
	}
}

// An event topic keeps every occurrence, up to its depth.
func TestEventTopicDeliversEveryMessage(t *testing.T) {
	b := bus.New()
	defer b.Close()
	ch, dropped := b.SubscribeEvent("session.notification", 8)

	for i := 1; i <= 5; i++ {
		b.PublishEvent("session.notification", i)
	}
	for i := 1; i <= 5; i++ {
		if got := recv(t, ch, "an event"); got != i {
			t.Fatalf("received %v, want %d", got, i)
		}
	}
	if n := dropped(); n != 0 {
		t.Errorf("dropped() = %d, want 0", n)
	}
}

// Overflow is reported, never silent: a queue that quietly forgets is a queue
// nobody can trust.
func TestEventTopicReportsWhatItDropped(t *testing.T) {
	b := bus.New()
	defer b.Close()
	ch, dropped := b.SubscribeEvent("task.output", 4)

	for i := 0; i < 20; i++ {
		b.PublishEvent("task.output", i)
	}
	if n := dropped(); n != 16 {
		t.Fatalf("dropped() = %d, want 16", n)
	}
	if got := recv(t, ch, "the oldest surviving event"); got != 0 {
		t.Errorf("received %v, want the queue to keep the oldest it could", got)
	}
}

// The rule that matters most: a subscriber that never reads must not be able
// to stall the thing publishing to it.
func TestASlowSubscriberNeverBlocksAPublisher(t *testing.T) {
	b := bus.New()
	defer b.Close()
	b.SubscribeState("session.usage")    // never read
	b.SubscribeEvent("session.usage", 1) // never read

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			b.PublishState("session.usage", i)
			b.PublishEvent("session.usage", i)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publishing blocked on a subscriber that never reads")
	}
}

func TestConcurrentUseIsSafe(t *testing.T) {
	b := bus.New()
	defer b.Close()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ch := b.SubscribeState("t")
			for j := 0; j < 200; j++ {
				b.PublishState("t", n*1000+j)
				select {
				case <-ch:
				default:
				}
			}
		}(i)
	}
	wg.Wait()
}

// A topic whose value is one fact about one thing cannot be a state topic.
//
// A state channel keeps only the latest value, which is right for a snapshot
// of everything — the pool publishes its whole list, and an older list is
// worth nothing. It is wrong for a fact about a single session: two sessions
// naming themselves within a moment of each other, and the second replaced the
// first before anybody read it. Nothing republishes a name, so the one that
// was dropped never arrived at all, and its tab kept the name of its
// directory for as long as the application ran.
func TestEveryEventArrivesWhereAStateWouldBeReplaced(t *testing.T) {
	b := bus.New()

	states := b.SubscribeState("topic.state")
	events, dropped := b.SubscribeEvent("topic.event", 8)

	for _, v := range []string{"first", "second", "third"} {
		b.PublishState("topic.state", v)
		b.PublishEvent("topic.event", v)
	}

	// The state channel holds one: whichever came last.
	var kept []string
	for len(states) > 0 {
		kept = append(kept, (<-states).(string))
	}
	if len(kept) != 1 || kept[0] != "third" {
		t.Errorf("the state channel held %v, want only the last", kept)
	}

	// The event channel holds all three, in order.
	var seen []string
	for i := 0; i < 3; i++ {
		select {
		case v := <-events:
			seen = append(seen, v.(string))
		case <-time.After(time.Second):
			t.Fatalf("only %v arrived", seen)
		}
	}
	if len(seen) != 3 || seen[0] != "first" || seen[2] != "third" {
		t.Errorf("the event channel held %v, want all three in order", seen)
	}
	if n := dropped(); n != 0 {
		t.Errorf("%d dropped from a channel with room", n)
	}
}
