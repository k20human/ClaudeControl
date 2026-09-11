package app

import (
	"time"

	"claudecontrol/internal/clipboard"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
)

// Selecting and the middle button, the way the rest of a Linux desktop does
// it.
//
// Two clipboards have existed for as long as X has: one you fill on purpose
// with ctrl+c, and one that fills itself the moment you select something and
// empties into whatever you middle-click. A terminal does this for you — until
// an application turns mouse reporting on, which takes the buttons away from
// it. This window takes the buttons, so it owes the behaviour back.
//
// The clipboard alt+c fills is untouched: dragging across a line must never
// cost you what you copied.

// primaryWait is how long a helper is given. It is generous for a program that
// answers in tens of milliseconds, and short enough that a desktop whose
// clipboard owner has gone away cannot hold up a paste.
const primaryWait = 300 * time.Millisecond

// takeSelection puts what a drag just selected where the rest of the desktop
// looks for it.
//
// Nothing is said about it in the status bar. Selecting is not an action
// anybody asked to be told about, and a notice on every drag would be noise
// over the one thing the bar is for.
func (a *App) takeSelection(id layout.PaneID) {
	m, ok := a.modules[id]
	if !ok {
		return
	}
	sel, ok := m.(interface{ SelectedText() string })
	if !ok {
		return
	}
	// An empty selection is a click, and a click does not empty the
	// selection: what you selected a minute ago is still what a middle click
	// should paste.
	text := sel.SelectedText()
	if text == "" || text == a.primary {
		return
	}
	a.primary = text

	// Handed to the desktop off the draw loop. The helper takes some tens of
	// milliseconds, which is a frame, and the end of a drag is exactly where a
	// dropped frame would be felt. Nothing is reported back because there is
	// nothing to report: what was selected is already remembered here, so the
	// middle button works whether or not a helper answered.
	go func() { _ = clipboard.WriteTo(clipboard.Primary, text, primaryWait) }()
}

// pasteSelection puts the primary selection into the focused pane.
//
// The desktop's is asked first, so text selected in a browser pastes here.
// What this window last selected is the fallback, which is the whole answer on
// a machine with no helper installed.
func (a *App) pasteSelection() {
	text, err := clipboard.ReadFrom(clipboard.Primary, primaryWait)
	if err != nil || text == "" {
		text = a.primary
	}
	if text == "" {
		a.setStatus("nothing selected — drag across some text first")
		return
	}
	m, ok := a.modules[a.focus]
	if !ok {
		return
	}
	in, ok := m.(module.Inputter)
	if !ok {
		a.setStatus("nothing in this pane takes text")
		return
	}
	in.Paste(text)
	a.Wake()
}
