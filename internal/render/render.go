// Package render holds the two drawing primitives every module needs. They
// lived in the application package until a module outside it needed them.
package render

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

// Fill paints a rectangle with a background colour.
func Fill(scr uv.Screen, area uv.Rectangle, bg color.Color) {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			cell := uv.EmptyCell
			cell.Content = " "
			cell.Style.Bg = bg
			scr.SetCell(x, y, &cell)
		}
	}
}

// Text paints a string, clipped to the screen bounds.
func Text(scr uv.Screen, x, y int, text string, fg, bg color.Color) {
	b := scr.Bounds()
	for _, r := range []rune(text) {
		if x >= b.Max.X {
			return
		}
		if x >= b.Min.X {
			cell := uv.EmptyCell
			cell.Content = string(r)
			cell.Style.Fg = fg
			cell.Style.Bg = bg
			scr.SetCell(x, y, &cell)
		}
		x++
	}
}
