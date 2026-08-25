package app

import (
	"fmt"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
)

var (
	barBg      = color.RGBA{R: 0x1b, G: 0x21, B: 0x2b, A: 0xff}
	barFg      = color.RGBA{R: 0x9a, G: 0xa8, B: 0xbd, A: 0xff}
	barHotBg   = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	barHotFg   = color.RGBA{R: 0x10, G: 0x16, B: 0x1e, A: 0xff}
	barQuitFg  = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
	barCountFg = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
)

// button is one clickable label in the status bar.
type button struct {
	label string
	rect  layout.Rect
	run   func(a *App)
	warn  bool // drawn in the warning colour
}

// buildStatusBar lays the buttons out from the left, and returns them with the
// rectangles a click is tested against.
//
// Widths come from ansi.StringWidth, not from len. A glyph can occupy two
// columns where its string holds one rune, and counting runes would shift every
// button after it — so the thing you click would stop being the thing you aimed
// at. Measuring the way the terminal measures is what makes icons safe here.
func (a *App) buildStatusBar() []button {
	items := []struct {
		label string
		run   func(a *App)
		warn  bool
	}{
		{"+ new", func(a *App) { _ = a.newPane(layout.Horizontal) }, false},
		{"× close", func(a *App) { _ = a.closePane(a.focus) }, false},
		{"▣ zoom", func(a *App) { a.toggleZoom() }, false},
		{"⇄ flip", func(a *App) { a.rotateFocusedSplit() }, false},
		{"≡ equal", func(a *App) { a.evenOutSplits() }, false},
		{"? help", func(a *App) { a.overlay = overlayHelp }, false},
	}

	y := a.area.H - 1
	out := make([]button, 0, len(items)+1)
	x := 1
	for _, it := range items {
		w := ansi.StringWidth(it.label) + 2
		out = append(out, button{
			label: it.label,
			rect:  layout.Rect{X: x, Y: y, W: w, H: 1},
			run:   it.run,
			warn:  it.warn,
		})
		x += w + 1
	}

	// Quit sits alone at the far right, away from "close", so that ending the
	// whole application is never one slip away from closing a single pane.
	const quit = "⏻ quit"
	qw := ansi.StringWidth(quit) + 2
	if qx := a.area.W - qw - 1; qx > x {
		out = append(out, button{
			label: quit,
			rect:  layout.Rect{X: qx, Y: y, W: qw, H: 1},
			run:   func(a *App) { a.overlay = overlayQuit },
			warn:  true,
		})
	}
	return out
}

// drawStatusBar paints the bar and its buttons.
func (a *App) drawStatusBar(scr uv.Screen) {
	y := a.area.H - 1
	fill(scr, layout.Rect{X: 0, Y: y, W: a.area.W, H: 1}, barBg)

	for i, b := range a.buttons {
		fg, bg := color.Color(barFg), color.Color(barBg)
		if b.warn {
			fg = barQuitFg
		}
		if i == a.hoverBtn {
			fg, bg = barHotFg, barHotBg
		}
		writeText(scr, b.rect.X, y, " "+b.label+" ", fg, bg)
	}

	// Pane count, right of the buttons and left of quit.
	count := fmt.Sprintf("%d panes", len(a.rects))
	if len(a.rects) == 1 {
		count = "1 pane"
	}
	if x := a.area.W - ansi.StringWidth("⏻ quit") - 4 - ansi.StringWidth(count) - 2; x > 0 {
		writeText(scr, x, y, count, barCountFg, barBg)
	}
}

// writeText paints a string, clipped to the screen bounds.
func writeText(scr uv.Screen, x, y int, text string, fg, bg color.Color) {
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

// buttonAt returns the index of the button under the pointer, or -1.
func buttonAt(buttons []button, x, y int) int {
	for i, b := range buttons {
		if x >= b.rect.X && x < b.rect.X+b.rect.W && y >= b.rect.Y && y < b.rect.Y+b.rect.H {
			return i
		}
	}
	return -1
}
