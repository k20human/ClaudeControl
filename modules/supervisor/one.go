package supervisor

// Acting on one service.
//
// The buttons across the top act on the ticked set, which is what you want for
// bringing a stack up or taking it down. A row acts on itself, which is what
// you want for the one server that has wedged — and getting there by unticking
// seven others and remembering to tick them back is not a gesture, it is a
// chore.

// StartOne brings up a single service, whatever is ticked.
func (m *Module) StartOne(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.atLocked(i); s != nil {
		m.startLocked(s)
	}
}

// StopOne takes down a single service, leaving the rest of the stack up.
func (m *Module) StopOne(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.atLocked(i); s != nil {
		m.stopLocked(s)
	}
}

// RestartOne stops and starts a single service.
//
// It goes through the same path the button does rather than stopping and
// starting in place: a restart has to wait for the old process to let go
// before the new one asks for its port, and that waiting is the whole
// difference between a restart and a race.
func (m *Module) RestartOne(i int) {
	m.mu.Lock()
	s := m.atLocked(i)
	if s == nil {
		m.mu.Unlock()
		return
	}
	was := make([]bool, len(m.svcs))
	for j, other := range m.svcs {
		was[j] = other.picked
		other.picked = other == s
	}
	m.mu.Unlock()

	m.RestartPicked()

	m.mu.Lock()
	for j, other := range m.svcs {
		if j < len(was) {
			other.picked = was[j]
		}
	}
	m.mu.Unlock()
}

// atLocked is the service at an index, or nil for an index nobody has.
func (m *Module) atLocked(i int) *service {
	if i < 0 || i >= len(m.svcs) {
		return nil
	}
	return m.svcs[i]
}

// Selected is the row the keyboard acts on.
func (m *Module) Selected() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sel
}
