package app

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
)

var (
	dividerFg = color.RGBA{R: 0x3a, G: 0x44, B: 0x55, A: 0xff}
	focusFg   = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
)

// drawChrome paints the divider strips, highlighting those that touch the
// focused pane so the focus is readable without a border around every pane.
func drawChrome(scr uv.Screen, rects map[layout.PaneID]layout.Rect, divs []layout.DividerRect, focus layout.PaneID) {
	fr, hasFocus := rects[focus]
	for _, d := range divs {
		fg := color.Color(dividerFg)
		if hasFocus && touches(d.Rect, fr) {
			fg = focusFg
		}
		glyph := "│"
		if d.Rect.W > 1 {
			glyph = "─"
		}
		for y := d.Rect.Y; y < d.Rect.Y+d.Rect.H; y++ {
			for x := d.Rect.X; x < d.Rect.X+d.Rect.W; x++ {
				cell := uv.EmptyCell
				cell.Content = glyph
				cell.Style.Fg = fg
				scr.SetCell(x, y, &cell)
			}
		}
	}
}

// touches reports whether a divider strip sits directly against a pane.
func touches(d, pane layout.Rect) bool {
	if d.W == 1 {
		return (d.X == pane.X+pane.W || d.X+1 == pane.X) &&
			d.Y < pane.Y+pane.H && pane.Y < d.Y+d.H
	}
	return (d.Y == pane.Y+pane.H || d.Y+1 == pane.Y) &&
		d.X < pane.X+pane.W && pane.X < d.X+d.W
}
