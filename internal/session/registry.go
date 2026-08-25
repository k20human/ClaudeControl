package session

import "sync"

// Registry holds every live session. Sessions live here, not in the layout
// tree: a session with no pane on screen keeps running.
type Registry struct {
	mu    sync.RWMutex
	items map[ID]*Session
	order []ID
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{items: make(map[ID]*Session)}
}

// Add stores a session, replacing any session with the same id.
func (r *Registry) Add(s *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[s.ID]; !exists {
		r.order = append(r.order, s.ID)
	}
	r.items[s.ID] = s
}

// Get returns the session with the given id.
func (r *Registry) Get(id ID) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.items[id]
	return s, ok
}

// Remove drops a session from the registry without closing it.
func (r *Registry) Remove(id ID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, id)
	for i, v := range r.order {
		if v == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// All returns every session in insertion order.
func (r *Registry) All() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Session, 0, len(r.order))
	for _, id := range r.order {
		if s, ok := r.items[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

// CloseAll closes every session and empties the registry. It returns the first
// error encountered, after attempting all of them.
func (r *Registry) CloseAll() error {
	var firstErr error
	for _, s := range r.All() {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	r.mu.Lock()
	r.items = make(map[ID]*Session)
	r.order = nil
	r.mu.Unlock()
	return firstErr
}
