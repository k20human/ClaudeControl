package app

import (
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
)

// focusWaiting goes to the next conversation that is waiting on you.
//
// "3 waiting" in the bar says that something needs you and not where it is,
// which with a screen of tabs is most of the question. This answers it: the
// pane is focused and, if it is a pane of tabs, the tab holding that
// conversation is brought to the front.
//
// It cycles. Pressed again it goes to the next one, so several waiting
// sessions are visited in turn rather than the same one twice.
func (a *App) focusWaiting() {
	waiting := a.waitingSessions()
	if len(waiting) == 0 {
		a.setStatus("nothing is waiting on you")
		return
	}

	leaves := layout.Leaves(a.root)
	start := 0
	for i, id := range leaves {
		if id == a.focus {
			// Begin after the pane you are on, so the first press moves.
			start = i + 1
			break
		}
	}

	for step := 0; step < len(leaves); step++ {
		id := leaves[(start+step)%len(leaves)]
		m, ok := a.modules[id]
		if !ok {
			continue
		}
		for _, held := range paneSessions(m) {
			if !waiting[a.liveSession(held)] && !waiting[held] {
				continue
			}
			a.setFocus(id)
			// A pane of tabs may be showing a different conversation than the
			// one that called: focusing the pane without changing the tab
			// would leave you looking at the wrong one.
			if sel, ok := m.(interface{ SelectSession(string) bool }); ok {
				sel.SelectSession(held)
			}
			a.Wake()
			return
		}
	}
	// The pool knows of a waiting session that no pane is showing: it is in
	// the sessions list rather than on screen, and that is where to go.
	a.setStatus("something is waiting in a session no pane is showing — try ◫ list")
}

// paneSessions is every conversation a pane holds.
//
// Two interfaces, because they mean different things. Sessioner is what a
// module publishes as worth bringing back, and a shell deliberately publishes
// nothing; SessionID is simply what is hosted here. Going to what is waiting
// is a question about what is hosted, so both are asked.
func paneSessions(m module.Module) []string {
	if lister, ok := m.(module.Sessioner); ok {
		if held := lister.Sessions(); len(held) > 0 {
			return held
		}
	}
	if one, ok := m.(interface{ SessionID() string }); ok {
		if id := one.SessionID(); id != "" {
			return []string{id}
		}
	}
	return nil
}

// waitingSessions is the set of session ids the pool reports as waiting.
func (a *App) waitingSessions() map[string]bool {
	if a.pool == nil {
		return nil
	}
	out := map[string]bool{}
	for _, e := range a.pool.All() {
		if e.State == pool.StateWaiting && e.Session != nil {
			out[string(e.Session.ID)] = true
		}
	}
	return out
}
