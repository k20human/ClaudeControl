package supervisor

import "claudecontrol/internal/session"

// The pane search, working on the log that is open.
//
// Opening it while the list is showing opens the highlighted service's log
// first: a search you have to set up before you can run is a search you will
// not run.

// logTarget is the session the search acts on: the log that is open, or the
// highlighted service's if the list is showing. Returns nils when that service
// is not running, which is the one case there is nothing to search.
//
// open says whether asking is allowed to change what the pane shows. Only the
// search itself may: the menu asks whether a search is possible every time you
// right-click, and a question must not move the pane you asked it about.
func (m *Module) logTarget(open bool) (*session.Session, *session.View) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.showing
	if i < 0 {
		i = m.sel
	}
	if i < 0 || i >= len(m.svcs) {
		return nil, nil
	}
	s := m.svcs[i]
	if s.sess == nil {
		return nil, nil
	}
	if open {
		m.showing = i
	}
	return s.sess, &s.view
}

// CanFind reports whether there is output to search right now. Without it the
// search bar would open over a pane that can never match anything.
func (m *Module) CanFind() bool {
	sess, _ := m.logTarget(false)
	return sess != nil
}

func (m *Module) Find(query string) int {
	sess, view := m.logTarget(true)
	if sess == nil {
		return 0
	}
	n := view.Find(sess, query)
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
	return n
}

func (m *Module) FindNext(delta int) {
	sess, view := m.logTarget(true)
	if sess == nil {
		return
	}
	view.FindNext(sess, delta)
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

func (m *Module) FindClear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.svcs {
		s.view.FindClear()
	}
}

func (m *Module) FindStatus() (string, int, int) {
	m.mu.Lock()
	i := m.showing
	if i < 0 || i >= len(m.svcs) {
		m.mu.Unlock()
		return "", 0, 0
	}
	view := &m.svcs[i].view
	m.mu.Unlock()
	return view.FindStatus()
}
