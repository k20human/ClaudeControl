package pool

import (
	"fmt"
	"sync"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/session"
)

// StateTopic is where the pool announces itself.
const StateTopic = "session.state"

// Entry is one session and what the list needs to show about it.
type Entry struct {
	Session  *session.Session
	Title    string
	Dir      string
	State    State
	Attached bool
}

// Pool owns every live session.
//
// Ownership sits here rather than in the layout tree so that removing a pane
// is a decision about the screen, not about a process. Moving, swapping and
// duplicating a pane become changes of binding; nothing has to be restarted.
type Pool struct {
	bus *bus.Bus

	mu    sync.RWMutex
	items map[session.ID]*Entry
	order []session.ID
}

// New returns an empty pool that announces itself on b.
func New(b *bus.Bus) *Pool {
	return &Pool{bus: b, items: make(map[session.ID]*Entry)}
}

// Add takes ownership of a session.
func (p *Pool) Add(s *session.Session, title, dir string) {
	p.mu.Lock()
	if _, exists := p.items[s.ID]; !exists {
		p.order = append(p.order, s.ID)
	}
	p.items[s.ID] = &Entry{Session: s, Title: title, Dir: dir}
	p.mu.Unlock()
	p.announce()
}

// Get returns a copy of one entry.
func (p *Pool) Get(id session.ID) (*Entry, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, ok := p.items[id]
	if !ok {
		return nil, false
	}
	c := *e
	return &c, true
}

// All returns copies of every entry, in the order they were added.
func (p *Pool) All() []*Entry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snapshotLocked()
}

// snapshotLocked copies the entries. Callers get their own structs so that
// writing to one cannot reach back into the pool's bookkeeping.
func (p *Pool) snapshotLocked() []*Entry {
	out := make([]*Entry, 0, len(p.order))
	for _, id := range p.order {
		if e, ok := p.items[id]; ok {
			c := *e
			out = append(out, &c)
		}
	}
	return out
}

// SetState records what a session is doing.
func (p *Pool) SetState(id session.ID, st State) {
	p.mu.Lock()
	e, ok := p.items[id]
	if ok {
		e.State = st
	}
	p.mu.Unlock()
	if ok {
		p.announce()
	}
}

// SetAttached records whether a pane is showing the session.
func (p *Pool) SetAttached(id session.ID, attached bool) {
	p.mu.Lock()
	e, ok := p.items[id]
	if ok {
		e.Attached = attached
	}
	p.mu.Unlock()
	if ok {
		p.announce()
	}
}

// Waiting counts the sessions that need an answer from the user.
func (p *Pool) Waiting() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := 0
	for _, e := range p.items {
		if e.State == StateWaiting {
			n++
		}
	}
	return n
}

// Kill ends a session and drops it.
func (p *Pool) Kill(id session.ID) error {
	p.mu.Lock()
	e, ok := p.items[id]
	if ok {
		delete(p.items, id)
		for i, v := range p.order {
			if v == id {
				p.order = append(p.order[:i], p.order[i+1:]...)
				break
			}
		}
	}
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("pool: session %s not found", id)
	}
	err := e.Session.Close()
	p.announce()
	return err
}

// CloseAll ends every session and empties the pool.
func (p *Pool) CloseAll() error {
	p.mu.Lock()
	entries := p.snapshotLocked()
	p.items = make(map[session.ID]*Entry)
	p.order = nil
	p.mu.Unlock()

	var firstErr error
	for _, e := range entries {
		if err := e.Session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	p.announce()
	return firstErr
}

// announce publishes a fresh snapshot on the state topic.
func (p *Pool) announce() {
	if p.bus == nil {
		return
	}
	p.mu.RLock()
	snap := p.snapshotLocked()
	p.mu.RUnlock()
	p.bus.PublishState(StateTopic, snap)
}
