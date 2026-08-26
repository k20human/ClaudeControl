package app

import (
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
)

var (
	paletteBg      = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	paletteFg      = color.RGBA{R: 0xc7, G: 0xd2, B: 0xe0, A: 0xff}
	paletteDim     = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	paletteAccent  = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	paletteRowBg   = color.RGBA{R: 0x24, G: 0x2e, B: 0x3d, A: 0xff}
	paletteQueryBg = color.RGBA{R: 0x1b, G: 0x21, B: 0x2b, A: 0xff}
)

// paletteEntry is one thing the palette can do.
type paletteEntry struct {
	label string
	desc  string
	run   func(a *App)
}

// paletteState is the open palette: what has been typed and what is highlighted.
type paletteState struct {
	query    string
	selected int
}

// paletteEntries is everything the palette offers.
//
// Built from the binding table, exactly as the help panel is. An action added
// there appears here without anyone remembering to add it, which is the only
// way a palette stays complete.
func (a *App) paletteEntries() []paletteEntry {
	out := make([]paletteEntry, 0, len(bindings))
	for _, b := range bindings {
		if b.desc == "" || b.run == nil {
			continue
		}
		out = append(out, paletteEntry{label: b.display, desc: b.desc, run: b.run})
	}
	return out
}

// paletteVisible is what the current query leaves.
func (a *App) paletteVisible() []paletteEntry {
	if a.palette == nil {
		return nil
	}
	var out []paletteEntry
	for _, e := range a.paletteEntries() {
		if matches(a.palette.query, e.label, e.desc) {
			out = append(out, e)
		}
	}
	return out
}

// matches reports whether a query selects an entry.
//
// It looks at the keystroke and the description alike: you remember one or the
// other — "the zoom one", "alt+something" — and rarely both.
func matches(query, label, desc string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(label), q) ||
		strings.Contains(strings.ToLower(desc), q)
}

// togglePalette opens or closes it.
func (a *App) togglePalette() {
	if a.palette != nil {
		a.palette = nil
		a.clearNext = true
		return
	}
	a.palette = &paletteState{}
	a.clearNext = true
}

// paletteType appends to the query.
func (a *App) paletteType(s string) {
	if a.palette == nil {
		return
	}
	a.palette.query += s
	a.palette.selected = 0
}

// paletteBackspace removes the last character of the query.
func (a *App) paletteBackspace() {
	if a.palette == nil || a.palette.query == "" {
		return
	}
	runes := []rune(a.palette.query)
	a.palette.query = string(runes[:len(runes)-1])
	a.palette.selected = 0
}

// paletteMove changes the highlighted row.
func (a *App) paletteMove(delta int) {
	if a.palette == nil {
		return
	}
	n := len(a.paletteVisible())
	a.palette.selected += delta
	if a.palette.selected >= n {
		a.palette.selected = n - 1
	}
	if a.palette.selected < 0 {
		a.palette.selected = 0
	}
}

// paletteChoose runs the highlighted action and closes.
//
// An empty list runs nothing and stays open: closing on a choice that could
// not be made would look like the action had been taken.
func (a *App) paletteChoose() {
	if a.palette == nil {
		return
	}
	visible := a.paletteVisible()
	if len(visible) == 0 {
		return
	}
	at := a.palette.selected
	if at < 0 || at >= len(visible) {
		at = 0
	}
	run := visible[at].run
	a.palette = nil
	a.clearNext = true
	run(a)
}

// palettePanelRect is where the palette is drawn.
func (a *App) palettePanelRect() layout.Rect {
	avail := a.paneArea()
	w, h := 56, 14
	if w > avail.W-4 {
		w = avail.W - 4
	}
	if h > avail.H-4 {
		h = avail.H - 4
	}
	if w < 20 {
		w = avail.W
	}
	if h < 5 {
		h = avail.H
	}
	return layout.Rect{
		X: avail.X + (avail.W-w)/2,
		Y: avail.Y + (avail.H-h)/2,
		W: w, H: h,
	}
}

// drawPalette paints the query line and the matching entries.
func (a *App) drawPalette(scr uv.Screen) {
	if a.palette == nil {
		return
	}
	r := a.palettePanelRect()
	render.Fill(scr, uv.Rect(r.X, r.Y, r.W, r.H), paletteBg)

	render.Fill(scr, uv.Rect(r.X, r.Y+1, r.W, 1), paletteQueryBg)
	render.Text(scr, r.X+2, r.Y+1, "› "+a.palette.query+"▌", paletteAccent, paletteQueryBg)

	visible := a.paletteVisible()
	if len(visible) == 0 {
		render.Text(scr, r.X+2, r.Y+3, "nothing matches", paletteDim, paletteBg)
	}
	for i, e := range visible {
		y := r.Y + 3 + i
		if y >= r.Y+r.H-1 {
			break
		}
		bg := color.Color(paletteBg)
		if i == a.palette.selected {
			bg = paletteRowBg
			render.Fill(scr, uv.Rect(r.X, y, r.W, 1), bg)
		}
		render.Text(scr, r.X+2, y, e.label, paletteAccent, bg)
		render.Text(scr, r.X+18, y, e.desc, paletteFg, bg)
	}
}
