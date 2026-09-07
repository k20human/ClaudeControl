package app

import (
	"strings"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
)

// Naming a tab by hand.
//
// Claude Code names a conversation after what it is about, and it is right
// nearly always. This is for the times it is not, and for a tab you think of
// as something else — "facturation" rather than the summary of the last turn.
//
// F2, and a double-click on the label: the two gestures every tabbed thing
// uses, so neither has to be learnt. The name is kept against the
// conversation, so it comes back tomorrow with what it names, and nothing
// published afterwards overwrites it. Clearing the line hands the tab back.

// renameState is the open prompt.
type renameState struct {
	pane  layout.PaneID
	index int
	name  string

	// was is the name the tab had when the prompt opened. Shown as the hint,
	// so what clearing the line will do is visible before you do it.
	was string

	// fresh says nothing has been typed yet, and the name on the line is the
	// one the tab already has. The first thing typed replaces it, the way the
	// selected text in a rename box anywhere else does — otherwise naming a
	// tab means clearing the line first, every time.
	fresh bool
}

// tabNamer is a pane whose tabs can be named.
type tabNamer interface {
	Rename(int, string) bool
	TitleAt(int) string
	ActiveIndex() int
	Count() int
}

// paneNamer is the pane's module if its tabs can be named.
func (a *App) paneNamer(id layout.PaneID) (tabNamer, bool) {
	m, ok := a.modules[id]
	if !ok {
		return nil, false
	}
	t, ok := m.(tabNamer)
	return t, ok
}

// beginRenameTab opens the prompt on the tab in front of you.
func (a *App) beginRenameTab() {
	t, ok := a.paneNamer(a.focus)
	if !ok {
		a.setStatus("this pane has no tabs to name")
		return
	}
	a.beginRenameTabAt(a.focus, t.ActiveIndex())
}

// beginRenameTabAt opens it on a particular tab, which is what a double-click
// on a label asks for.
func (a *App) beginRenameTabAt(id layout.PaneID, i int) {
	t, ok := a.paneNamer(id)
	if !ok || i < 0 || i >= t.Count() {
		return
	}
	title := t.TitleAt(i)
	a.rename = &renameState{pane: id, index: i, name: title, was: title, fresh: true}
	a.Wake()
}

func (a *App) closeRename() {
	if a.rename == nil {
		return
	}
	a.rename = nil
	a.clearNext = true
	a.Wake()
}

// renameKey handles the prompt, and reports whether it took the key.
func (a *App) renameKey(e uv.KeyPressEvent) bool {
	if a.rename == nil {
		return false
	}
	key := e.Key()
	switch {
	case key.Code == uv.KeyEscape:
		a.closeRename()
	case key.Code == uv.KeyEnter:
		a.applyRename()
	case key.Code == uv.KeyBackspace:
		// Editing what is there, rather than replacing it: backspace is how
		// you say "keep this name, change the end of it".
		a.rename.fresh = false
		if n := len(a.rename.name); n > 0 {
			_, size := utf8.DecodeLastRuneInString(a.rename.name)
			a.rename.name = a.rename.name[:n-size]
			a.Wake()
		}
	case key.Text != "":
		if a.rename.fresh {
			a.rename.name = ""
			a.rename.fresh = false
		}
		a.rename.name += key.Text
		a.Wake()
	}
	return true
}

// applyRename gives the tab the name, and writes it down.
//
// Written at once rather than on the way out: a name is worth keeping even if
// the run ends badly, and it costs one small file.
func (a *App) applyRename() {
	r := a.rename
	a.closeRename()
	if r == nil {
		return
	}
	t, ok := a.paneNamer(r.pane)
	if !ok {
		return
	}
	if !t.Rename(r.index, r.name) {
		return
	}
	a.saveNames()
	a.Wake()
}

// renamePromptRect is where the prompt sits: over the strip of the pane whose
// tab is being named, because that is what the name is for and looking
// somewhere else to type it would be strange.
func (a *App) renamePromptRect() layout.Rect {
	r := a.contentRect(a.rename.pane)
	w := 44
	if w > r.W {
		w = r.W
	}
	return layout.Rect{X: r.X, Y: r.Y, W: w, H: 1}
}

func (a *App) drawRename(scr uv.Screen) {
	if a.rename == nil {
		return
	}
	r := a.renamePromptRect()
	render.Fill(scr, uv.Rect(r.X, r.Y, r.W, r.H), dirQueryBg)

	label := "name: "
	render.Text(scr, r.X, r.Y, label, dirDim, dirQueryBg)
	at := r.X + ansi.StringWidth(label)
	room := r.X + r.W - at - 1
	if room < 1 {
		return
	}
	typed := a.rename.name + "▌"
	if ansi.StringWidth(typed) > room {
		typed = ansi.Truncate(typed, room, "")
	}
	render.Text(scr, at, r.Y, typed, dirAccent, dirQueryBg)

	// What clearing the line would do, said where there is room to say it.
	hint := "esc cancels · empty name gives it back"
	switch {
	case strings.TrimSpace(a.rename.name) == "":
		hint = "empty: back to " + a.rename.was
	case a.rename.fresh:
		hint = "type to replace · enter keeps this name"
	}
	if x := r.X + r.W - ansi.StringWidth(hint) - 1; x > at+ansi.StringWidth(typed)+2 {
		render.Text(scr, x, r.Y, hint, dirDim, dirQueryBg)
	}
}
