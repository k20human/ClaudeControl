package app

import (
	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
)

// binding maps a keystroke to an action. Every keystroke listed here was
// verified absent from the Claude Code 2.1.245 binary; anything not listed is
// forwarded to the guest untouched.
type binding struct {
	keys []string
	run  func(a *App)
}

var bindings = []binding{
	{[]string{"alt+h"}, func(a *App) { a.focusDirection(Left) }},
	{[]string{"alt+l"}, func(a *App) { a.focusDirection(Right) }},
	{[]string{"alt+k"}, func(a *App) { a.focusDirection(Up) }},
	{[]string{"alt+j"}, func(a *App) { a.focusDirection(Down) }},
	{[]string{"alt+`"}, func(a *App) { a.setFocus(a.prev) }},
}

func (a *App) handleKey(e uv.KeyPressEvent) {
	for _, b := range bindings {
		if e.MatchString(b.keys...) {
			b.run(a)
			return
		}
	}
	if m, ok := a.modules[a.focus].(module.Inputter); ok {
		m.Key(e)
	}
}

// focusDirection moves focus to the neighbouring pane, if there is one.
func (a *App) focusDirection(d Direction) {
	a.setFocus(nearest(a.rects, a.focus, d))
}

// setFocus changes focus, remembering the previous pane for alt+`.
func (a *App) setFocus(id layout.PaneID) {
	if id == a.focus {
		return
	}
	if _, ok := a.rects[id]; !ok {
		return
	}
	a.prev = a.focus
	a.focus = id
}
