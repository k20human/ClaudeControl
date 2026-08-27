package tasks

// The pane search, working on the task on screen — one that is still running,
// or one that has finished and left its output there. A task prints a great
// deal and the line that matters is rarely the last one.

// CanFind reports whether there is output to search. A task that has never run
// has none, and a search bar over nothing can only answer "none".
func (m *Module) CanFind() bool { return m.running != nil }

func (m *Module) Find(query string) int {
	if m.running == nil {
		return 0
	}
	n := m.view.Find(m.running, query)
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
	return n
}

func (m *Module) FindNext(delta int) {
	if m.running == nil {
		return
	}
	m.view.FindNext(m.running, delta)
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

func (m *Module) FindClear() { m.view.FindClear() }

func (m *Module) FindStatus() (string, int, int) { return m.view.FindStatus() }
