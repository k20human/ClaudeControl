package tabs

import (
	"fmt"

	"claudecontrol/internal/module"
)

// Moving a tab: out of a pane, into a pane, and along a strip.
//
// What travels is the module instance, and nothing is re-initialised. That is
// the load-bearing decision here. A Claude module's identity is a uuid made in
// Init — it names the transcript, it is what the hooks report, and it is what
// the pool keys state on — so handing a moved module a fresh Init would orphan
// the session it is showing. A moved module keeps the context it was given,
// including the pane id in it, which is used only as a title of last resort.

// TabAt is the tab under a point in the pane's own coordinates, and whether
// there is one.
//
// The cross and the plus are not tabs: a press on either is a button being
// pressed, and starting to drag from one would take the gesture away from it.
func (m *Module) TabAt(x, y int) (int, bool) {
	if y < 0 || y >= stripRows {
		return 0, false
	}
	kind, i := m.stripAt(x)
	if kind != hitTab {
		return 0, false
	}
	return i, true
}

// Detach removes a tab and hands back what was in it, running.
//
// It closes nothing: a detached module is on its way somewhere else. The
// caller owns it until it is adopted, and must close it if the move falls
// through.
//
// The pane may be left with no tabs at all. That is not an error here — the
// application takes the empty pane out of the layout, which is what dragging
// the last tab away is asking for.
func (m *Module) Detach(i int) (module.Module, string, bool) {
	m.mu.Lock()
	if i < 0 || i >= len(m.tabs) {
		m.mu.Unlock()
		return nil, "", false
	}
	going := m.tabs[i]
	m.tabs = append(m.tabs[:i:i], m.tabs[i+1:]...)
	switch {
	case len(m.tabs) == 0:
		m.active = 0
	case m.active >= len(m.tabs):
		m.active = len(m.tabs) - 1
	case m.active > i:
		m.active--
	}
	m.mu.Unlock()

	m.wake()
	return going.mod, going.title, true
}

// Adopt puts a module that is already running into a new tab, which becomes
// the one on screen: a tab that arrived somewhere you then have to go and find
// is a strange kind of arrival.
//
// Init is not called. It has been, wherever this module came from.
func (m *Module) Adopt(mod module.Module, title string) error {
	if mod == nil {
		return fmt.Errorf("tabs: nothing to adopt")
	}
	m.mu.Lock()
	if title == "" {
		title = titleFor(mod, "tab")
	}
	title = m.uniqueTitleLocked(title)
	inner := m.rows - stripRows
	if inner < 1 {
		inner = 1
	}
	m.tabs = append(m.tabs, &tab{title: title, name: nameOf(mod), mod: mod})
	m.active = len(m.tabs) - 1
	cols := m.cols
	m.mu.Unlock()

	// Sized for its new home. A failure is worth saying but not worth
	// refusing the move over: the tab is here, and putting it back where it
	// came from would be a second move nobody asked for.
	if cols > 0 {
		if err := mod.Resize(cols, inner); err != nil && m.ctx.Status != nil {
			m.ctx.Status(title + ": " + err.Error())
		}
	}
	m.wake()
	return nil
}

// Reorder moves a tab along the strip, leaving you looking at the one you
// moved.
func (m *Module) Reorder(from, to int) {
	m.mu.Lock()
	n := len(m.tabs)
	if from < 0 || from >= n || to < 0 || to >= n || from == to {
		m.mu.Unlock()
		return
	}
	moving := m.tabs[from]
	rest := append(m.tabs[:from:from], m.tabs[from+1:]...)
	m.tabs = append(rest[:to:to], append([]*tab{moving}, rest[to:]...)...)
	m.active = to
	m.mu.Unlock()

	m.wake()
}

// nameOf is the module name a moved tab is recorded under, so a pane written
// back to the configuration says what it holds. Unknown for a module that
// arrived from somewhere that did not say.
func nameOf(mod module.Module) string {
	if n, ok := mod.(interface{ ModuleName() string }); ok {
		return n.ModuleName()
	}
	return ""
}

// wake asks for a repaint.
func (m *Module) wake() {
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}
