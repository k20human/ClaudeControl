package app

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
)

var (
	findBg    = color.RGBA{R: 0x1b, G: 0x21, B: 0x2b, A: 0xff}
	findFg    = color.RGBA{R: 0x9a, G: 0xa8, B: 0xbd, A: 0xff}
	findLabel = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	findNone  = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
)

// finder is what a pane must offer for its history to be searched.
type finder interface {
	Find(string) int
	FindNext(int)
	FindClear()
	FindStatus() (string, int, int)
}

// findState is an open search over one pane.
//
// It belongs to a pane rather than to the application: what you are looking
// for is in that pane's history, and moving the focus while searching would
// leave a query pointing at output it never came from.
type findState struct {
	pane  layout.PaneID
	query string
	found int
}

// toggleFind opens the search on the focused pane, or closes it.
func (a *App) toggleFind() {
	if a.find != nil {
		a.closeFind()
		return
	}
	if _, ok := a.paneFinder(a.focus); !ok {
		a.setStatus("this pane has no history to search")
		return
	}
	a.find = &findState{pane: a.focus}
	a.Wake()
}

// closeFind ends the search, leaving the view where it left you: you found
// what you wanted, and being thrown back to the bottom would undo that.
func (a *App) closeFind() {
	if a.find == nil {
		return
	}
	if f, ok := a.paneFinder(a.find.pane); ok {
		f.FindClear()
	}
	a.find = nil
	a.clearNext = true
	a.Wake()
}

func (a *App) paneFinder(id layout.PaneID) (finder, bool) {
	m, ok := a.modules[id]
	if !ok {
		return nil, false
	}
	f, ok := m.(finder)
	if !ok {
		return nil, false
	}
	// A pane that can search in principle may have nothing to search now — a
	// task that has not been run, a service that is down. Opening a bar over
	// one of those would offer a search that can only ever answer "none".
	if c, asked := f.(interface{ CanFind() bool }); asked && !c.CanFind() {
		return nil, false
	}
	return f, true
}

// findKey drives the search bar and reports whether it took the key.
//
// While it is open every key belongs to it, because you are typing a query and
// a query is text: a letter that reached the guest would be a letter missing
// from what you meant to find.
func (a *App) findKey(e uv.KeyPressEvent) bool {
	if a.find == nil {
		return false
	}
	key := e.Key()
	switch {
	case key.Code == uv.KeyEscape:
		a.closeFind()
	case key.Code == uv.KeyEnter:
		a.findMove(1)
	case key.Code == uv.KeyBackspace:
		if n := len(a.find.query); n > 0 {
			a.find.query = a.find.query[:n-1]
			a.runFind()
		}
	case key.Code == uv.KeyUp:
		a.findMove(-1)
	case key.Code == uv.KeyDown:
		a.findMove(1)
	case key.Text != "":
		a.find.query += key.Text
		a.runFind()
	}
	return true
}

func (a *App) runFind() {
	f, ok := a.paneFinder(a.find.pane)
	if !ok {
		a.closeFind()
		return
	}
	a.find.found = f.Find(a.find.query)
	a.Wake()
}

func (a *App) findMove(delta int) {
	f, ok := a.paneFinder(a.find.pane)
	if !ok || a.find.found == 0 {
		return
	}
	f.FindNext(delta)
	a.Wake()
}

// drawFind paints the search bar along the bottom of the pane being searched.
func (a *App) drawFind(scr uv.Screen) {
	if a.find == nil {
		return
	}
	r, ok := a.rects[a.find.pane]
	if !ok || r.H < 1 {
		a.closeFind()
		return
	}
	y := r.Y + r.H - 1
	render.Fill(scr, uv.Rect(r.X, y, r.W, 1), findBg)

	const label = " find "
	render.Text(scr, r.X, y, label, findLabel, findBg)
	x := r.X + ansi.StringWidth(label)

	query := a.find.query + "▏"
	render.Text(scr, x, y, clipBar(query, r.W-ansi.StringWidth(label)-12), findFg, findBg)

	// The count on the right, and nothing pretending to be a count before
	// there is a query to count.
	if a.find.query == "" {
		return
	}
	fg, count := color.Color(findFg), ""
	if _, at, total := a.paneFindStatus(); total > 0 {
		count = itoa(at) + "/" + itoa(total)
	} else {
		count, fg = "none", findNone
	}
	if at := r.X + r.W - ansi.StringWidth(count) - 1; at > x {
		render.Text(scr, at, y, count, fg, findBg)
	}
}

func (a *App) paneFindStatus() (string, int, int) {
	f, ok := a.paneFinder(a.find.pane)
	if !ok {
		return "", 0, 0
	}
	return f.FindStatus()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
