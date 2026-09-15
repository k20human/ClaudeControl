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
	// Attached from the start: a session is added by the pane that opened it,
	// and it is the pane that later says it has let go. Left false until the
	// caller said otherwise, an entry counted as detached for as long as that
	// took — and what is counted is what the window reports about itself.
	p.items[s.ID] = &Entry{Session: s, Title: title, Dir: dir, Attached: true}
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

// stateOf is what a session is doing, with the process having the last word.
//
// The hooks are reports about a process; the process is the fact. A
// conversation that ended while it was waiting — killed, crashed, or closed
// without its last hook arriving — used to keep the word "waiting" for as long
// as the application stayed open: the title said "2 waiting" over a single
// running session, and the key that goes to whatever is waiting had nowhere to
// go.
//
// The entry is kept either way. A session that has ended is still worth
// listing, and saying so is the point; saying it is waiting for you is not.
func stateOf(e *Entry) State {
	if e == nil {
		return StateIdle
	}
	if e.Session != nil {
		if st, _ := e.Session.Status(); st == session.Exited {
			return StateExited
		}
	}
	return e.State
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
			c.State = stateOf(e)
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

// Waiting counts the sessions on screen that need an answer from the user.
//
// On screen: a conversation whose pane was closed keeps running — that is what
// closing a tab does to a Claude session — and it is still listed, marked
// detached, where the sessions are listed. It is not what this window is
// waiting on, and counting it said "5 waiting" to somebody looking at two.
//
// It is also what the count is for. The number is a button that goes to what
// is waiting, and nothing can go to a conversation no pane is showing.
func (p *Pool) Waiting() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := 0
	for _, e := range p.items {
		if e.Attached && stateOf(e) == StateWaiting {
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
