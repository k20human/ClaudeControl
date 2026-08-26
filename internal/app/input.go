package app

import (
	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
)

// binding maps a keystroke to an action. Every keystroke listed here was
// verified absent from the Claude Code 2.1.245 binary; anything not listed is
// forwarded to the guest untouched.
//
// display and desc are what the help panel shows, so the panel is generated
// from this table rather than written alongside it.
type binding struct {
	keys    []string
	display string
	desc    string
	run     func(a *App)
}

var bindings = []binding{
	{[]string{"alt+h", "alt+j", "alt+k", "alt+l"}, "alt+h j k l", "move focus", nil},
	{[]string{"alt+h"}, "", "", func(a *App) { a.focusDirection(Left) }},
	{[]string{"alt+l"}, "", "", func(a *App) { a.focusDirection(Right) }},
	{[]string{"alt+k"}, "", "", func(a *App) { a.focusDirection(Up) }},
	{[]string{"alt+j"}, "", "", func(a *App) { a.focusDirection(Down) }},
	{[]string{"alt+`"}, "alt+`", "previous pane", func(a *App) { a.setFocus(a.prev) }},
	{[]string{"alt+n"}, "alt+n", "new session", func(a *App) { _ = a.newPane(layout.Horizontal) }},
	{[]string{"alt+x"}, "alt+x", "close pane", func(a *App) { _ = a.closePane(a.focus) }},
	{[]string{"alt+z"}, "alt+z", "zoom / restore", func(a *App) { a.toggleZoom() }},
	{[]string{"alt+m"}, "alt+m", "flip the split: side by side <-> stacked", func(a *App) { a.rotateFocusedSplit() }},
	{[]string{"alt+s"}, "alt+s", "reset every split to equal shares", func(a *App) { a.evenOutSplits() }},
	{[]string{"alt+space"}, "alt+space", "sessions", func(a *App) { a.toggleSessionPanel() }},
	{[]string{"alt+,"}, "alt+,", "settings", func(a *App) { a.toggleSettingsPanel() }},
	{[]string{"alt+g"}, "alt+g", "this panel", func(a *App) { a.overlay = overlayHelp }},
	{[]string{"alt+q"}, "alt+q", "quit", func(a *App) { a.overlay = overlayQuit }},
}

func (a *App) handleKey(e uv.KeyPressEvent) {
	// The menu takes the keyboard while it is open. Plain arrows adjust,
	// because a modifier on every nudge would make tuning a slider a chore.
	if a.settingsPanel != nil {
		switch {
		case e.MatchString("esc", "alt+,"):
			a.toggleSettingsPanel()
		case e.MatchString("up", "k"):
			a.settingsPanel.Move(-1)
		case e.MatchString("down", "j"):
			a.settingsPanel.Move(1)
		case e.MatchString("left", "h"):
			_ = a.settingsPanel.Adjust(-1)
		case e.MatchString("right", "l"):
			_ = a.settingsPanel.Adjust(1)
		case e.MatchString("shift+left"):
			_ = a.settingsPanel.Adjust(-10)
		case e.MatchString("shift+right"):
			_ = a.settingsPanel.Adjust(10)
		case e.MatchString("s"):
			_ = a.saveSettings()
		}
		return
	}

	// The session list takes the keyboard while it is open, so its plain keys
	// stay plain: no modifier needed to attach or detach.
	if a.sessionPanel != nil {
		switch {
		case e.MatchString("esc", "alt+space"):
			a.toggleSessionPanel()
		case e.MatchString("up", "k"):
			a.sessionPanel.MoveSelection(-1)
		case e.MatchString("down", "j"):
			a.sessionPanel.MoveSelection(1)
		case e.MatchString("enter"):
			a.attachSelected()
		case e.MatchString("d"):
			a.detachSelected()
		case e.MatchString("x"):
			a.killSelected()
		}
		return
	}

	// A panel swallows the keystroke that closes it, so dismissing help can
	// never drop a stray character into the session underneath.
	if a.overlay != overlayNone {
		a.dismissOverlay(e.MatchString("y", "enter"))
		return
	}
	for _, b := range bindings {
		if b.run != nil && e.MatchString(b.keys...) {
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
