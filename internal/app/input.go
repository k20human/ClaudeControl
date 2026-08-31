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
	// Each direction carries its own description rather than sharing one
	// display-only line. The palette can only offer an entry that does
	// something, so a combined row would leave moving focus unreachable there.
	{[]string{"alt+h"}, "alt+h", "move focus left", func(a *App) { a.focusDirection(Left) }},
	{[]string{"alt+j"}, "alt+j", "move focus down", func(a *App) { a.focusDirection(Down) }},
	{[]string{"alt+k"}, "alt+k", "move focus up", func(a *App) { a.focusDirection(Up) }},
	{[]string{"alt+l"}, "alt+l", "move focus right", func(a *App) { a.focusDirection(Right) }},
	{[]string{"alt+`"}, "alt+`", "previous pane", func(a *App) { a.setFocus(a.prev) }},
	{[]string{"alt+n"}, "alt+n", "new session", func(a *App) { _ = a.newPane(layout.Horizontal) }},
	{[]string{"alt+x"}, "alt+x", "close tab, or the pane", func(a *App) { a.closeFocused() }},
	{[]string{"alt+a"}, "alt+a", "add a tab", func(a *App) { a.addTab() }},
	{[]string{"alt+z"}, "alt+z", "zoom / restore", func(a *App) { a.toggleZoom() }},
	{[]string{"alt+m"}, "alt+m", "flip the split: side by side <-> stacked", func(a *App) { a.rotateFocusedSplit() }},
	{[]string{"alt+s"}, "alt+s", "reset every split to equal shares", func(a *App) { a.evenOutSplits() }},
	{[]string{"alt+space"}, "alt+space", "sessions", func(a *App) { a.toggleSessionPanel() }},
	{[]string{"alt+,"}, "alt+,", "settings", func(a *App) { a.toggleSettingsPanel() }},
	{[]string{"alt+/"}, "alt+/", "command palette", func(a *App) { a.togglePalette() }},
	// The fallback for a terminal that swallows alt+click, and the way to
	// start a move without holding a button down. It picks the focused pane
	// up; the next click, anywhere, chooses where it lands.
	{[]string{"alt+r"}, "alt+r", "move this pane, then click a target", func(a *App) { a.beginPaneDrag(a.focus) }},
	// Absent from the Claude Code binary, like the rest — and, unlike the
	// obvious candidates, meaningless as escape sequences. ESC = is DECKPAM,
	// ESC - designates a character set, ESC c is a full reset and ESC [ is
	// CSI itself: a decoder reads all four as sequences rather than as keys,
	// which was found by trying alt+= and watching nothing happen.
	{[]string{"alt+;"}, "alt+;", "previous tab", func(a *App) { a.cycleTab(-1) }},
	{[]string{"alt+'"}, "alt+'", "next tab", func(a *App) { a.cycleTab(1) }},
	// Free in the Claude Code binary, meaningless as an escape sequence, not a
	// readline binding — alt+b and alt+f move by word in every shell — and
	// unshifted on an AZERTY keyboard.
	{[]string{"alt+:"}, "alt+:", "search your conversations", func(a *App) { a.toggleConvSearch() }},
	// No keys of its own: the pane it searches is the one you right-clicked,
	// and the click is what says which. Listed here all the same so the help
	// panel and the palette know about it — an action reachable only from a
	// menu nobody opens is an action nobody has.
	{nil, "right-click", "find in this pane", func(a *App) { a.toggleFind() }},
	// Absent from the Claude Code 2.1.247 binary, and it reaches the
	// application intact through a hosting emulator — ESC c is a full reset
	// only on the way out to a terminal, never on the way in. It does shadow
	// readline's capitalize-word in a shell, which is the price of the one
	// letter everybody already associates with copying.
	{[]string{"alt+c"}, "alt+c", "copy the selection", func(a *App) { a.copySelection() }},
	// Free in the Claude Code binary, unbound in readline, and it arrives
	// intact through a hosting emulator — the three tests every key here has
	// to pass. i for the input a session is waiting for.
	{[]string{"alt+i"}, "alt+i", "go to what is waiting on you", func(a *App) { a.focusWaiting() }},
	{[]string{"alt+g"}, "alt+g", "this panel", func(a *App) { a.overlay = overlayHelp }},
	{[]string{"alt+q"}, "alt+q", "quit", func(a *App) { a.overlay = overlayQuit }},
}

// closeFocused closes the tab on screen, or the pane when there is no tab to
// close.
//
// The same key for both, because it is the same intent: get rid of what I am
// looking at. A terminal closes its window with the last tab, and this is that
// gesture — the pane goes when its last tab would have.
func (a *App) closeFocused() {
	m, ok := a.modules[a.focus]
	if !ok {
		return
	}
	t, ok := m.(interface {
		CloseTab(int) error
		ActiveIndex() int
		Count() int
	})
	if !ok || t.Count() <= 1 {
		_ = a.closePane(a.focus)
		return
	}
	if err := t.CloseTab(t.ActiveIndex()); err != nil {
		a.setStatus("%s", err)
	}
}

// addTab opens one in the focused pane, if that pane is a pane of tabs.
func (a *App) addTab() {
	m, ok := a.modules[a.focus]
	if !ok {
		return
	}
	t, ok := m.(interface {
		Add(string, map[string]any) error
	})
	if !ok {
		a.setStatus("this pane has no tabs — alt+n opens a new pane")
		return
	}
	// The same thing a new pane holds: a session is what you open a tab for.
	if err := t.Add("claude", nil); err != nil {
		a.setStatus("%s", err)
	}
}

// cycleTab moves through the tabs of the focused pane, if it has any.
func (a *App) cycleTab(delta int) {
	m, ok := a.modules[a.focus]
	if !ok {
		return
	}
	t, ok := m.(interface{ CycleTab(int) })
	if !ok {
		a.setStatus("this pane has no tabs")
		return
	}
	t.CycleTab(delta)
}

func (a *App) handleKey(e uv.KeyPressEvent) {
	if a.menuKey(e) {
		return
	}
	if a.findKey(e) {
		return
	}
	if a.convKey(e) {
		return
	}

	// A pane or a tab in flight takes escape, and nothing else: every other
	// key still reaches the guest, so a drag started by accident costs one
	// keystroke.
	if a.paneDrag != nil && e.MatchString("esc") {
		a.cancelPaneDrag()
		return
	}
	if a.tabDrag != nil && e.MatchString("esc") {
		a.cancelTabDrag()
		return
	}

	// The palette takes every keystroke: it is a text field, so a plain letter
	// has to reach the query rather than an action. Only escape and its own
	// binding close it.
	if a.palette != nil {
		key := e.Key()
		switch {
		case e.MatchString("esc", "alt+/"):
			a.togglePalette()
		case key.Code == uv.KeyEnter:
			a.paletteChoose()
		case key.Code == uv.KeyBackspace:
			a.paletteBackspace()
		case key.Code == uv.KeyUp:
			a.paletteMove(-1)
		case key.Code == uv.KeyDown:
			a.paletteMove(1)
		case key.Text != "":
			a.paletteType(key.Text)
		}
		return
	}

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
		// Space is the box, not an answer: a panel offering a choice has to
		// let you change it without leaving.
		if a.overlay == overlayQuit && e.MatchString("space") {
			a.keepLayout = !a.keepLayout
			a.Wake()
			return
		}
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
