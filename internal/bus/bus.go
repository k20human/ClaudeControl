// Package bus separates what produces a fact from what displays it.
//
// Two semantics, and confusing them is the classic mistake. A state topic
// carries a current value: only the newest matters, and a subscriber that fell
// behind should see the present rather than a history it cannot use. An event
// topic carries occurrences: each one counts, the queue is bounded, and what
// it could not keep is counted rather than forgotten.
package bus

import "sync"

// Bus routes published values to subscribers.
type Bus struct {
	mu     sync.RWMutex
	states map[string][]chan any
	events map[string][]*eventSub
	closed bool
}

type eventSub struct {
	ch      chan any
	mu      sync.Mutex
	dropped int
}

// New returns an empty bus.
func New() *Bus {
	return &Bus{
		states: make(map[string][]chan any),
		events: make(map[string][]*eventSub),
	}
}

// SubscribeState returns a channel carrying the latest value of a topic.
//
// The channel has room for one value, and a publisher replaces what is waiting
// there rather than queueing behind it.
func (b *Bus) SubscribeState(topic string) <-chan any {
	ch := make(chan any, 1)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(ch)
		return ch
	}
	b.states[topic] = append(b.states[topic], ch)
	return ch
}

// SubscribeEvent returns a channel carrying every occurrence on a topic, up to
// depth, and a function reporting how many had to be dropped.
func (b *Bus) SubscribeEvent(topic string, depth int) (<-chan any, func() int) {
	if depth < 1 {
		depth = 1
	}
	s := &eventSub{ch: make(chan any, depth)}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(s.ch)
		return s.ch, func() int { return 0 }
	}
	b.events[topic] = append(b.events[topic], s)
	return s.ch, func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.dropped
	}
}

// PublishState replaces the pending value on a state topic. It never blocks.
func (b *Bus) PublishState(topic string, v any) {
	b.mu.RLock()
	subs := b.states[topic]
	b.mu.RUnlock()
	for _, ch := range subs {
		// Drain then fill. The drain can lose a race with a reader, which is
		// harmless: the worst case is that the send below finds room anyway.
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- v:
		default:
		}
	}
}

// PublishEvent queues an occurrence. It never blocks; a full queue counts the
// loss instead.
func (b *Bus) PublishEvent(topic string, v any) {
	b.mu.RLock()
	subs := b.events[topic]
	b.mu.RUnlock()
	for _, s := range subs {
		select {
		case s.ch <- v:
		default:
			s.mu.Lock()
			s.dropped++
			s.mu.Unlock()
		}
	}
}

// Close releases every subscriber. Publishing afterwards is a no-op.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for _, subs := range b.states {
		for _, ch := range subs {
			close(ch)
		}
	}
	for _, subs := range b.events {
		for _, s := range subs {
			close(s.ch)
		}
	}
	b.states = make(map[string][]chan any)
	b.events = make(map[string][]*eventSub)
}
