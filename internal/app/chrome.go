package app

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
)

var (
	dividerBg = color.RGBA{R: 0x24, G: 0x2b, B: 0x38, A: 0xff}
	focusBg   = color.RGBA{R: 0x35, G: 0x45, B: 0x5c, A: 0xff}
	hoverBg   = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
)

// drawDividers paints the strips between panes.
//
// They are filled bars rather than line glyphs: a bar reads as something you
// can take hold of, and it makes the hover state a plain change of colour
// instead of a change of character.
func drawDividers(scr uv.Screen, rects map[layout.PaneID]layout.Rect, divs []layout.DividerRect, focus layout.PaneID, hovered int) {
	fr, hasFocus := rects[focus]
	for i, d := range divs {
		bg := color.Color(dividerBg)
		switch {
		case i == hovered:
			bg = hoverBg
		case hasFocus && touches(d.Rect, fr):
			bg = focusBg
		}
		render.Fill(scr, uv.Rect(d.Rect.X, d.Rect.Y, d.Rect.W, d.Rect.H), bg)
	}
}

// touches reports whether a divider strip sits directly against a pane.
func touches(d, pane layout.Rect) bool {
	horizontal := d.H > d.W || (d.W == d.H && d.H > 1)
	if horizontal {
		return (d.X == pane.X+pane.W || d.X+d.W == pane.X) &&
			d.Y < pane.Y+pane.H && pane.Y < d.Y+d.H
	}
	return (d.Y == pane.Y+pane.H || d.Y+d.H == pane.Y) &&
		d.X < pane.X+pane.W && pane.X < d.X+d.W
}
