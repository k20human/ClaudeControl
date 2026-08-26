package app

import (
	"fmt"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
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
	// Ordered by what would be missed most, because a narrow bar drops from
	// the end. Help goes early despite being the least used: it is how the
	// rest is discovered, and a bar that drops it leaves nothing to ask.
	items := []struct {
		label string
		run   func(a *App)
		warn  bool
	}{
		{"+ new", func(a *App) { _ = a.newPane(layout.Horizontal) }, false},
		{"× close", func(a *App) { _ = a.closePane(a.focus) }, false},
		{"◫ list", func(a *App) { a.toggleSessionPanel() }, false},
		{"? help", func(a *App) { a.overlay = overlayHelp }, false},
		{"⚙ set", func(a *App) { a.toggleSettingsPanel() }, false},
		{"▣ zoom", func(a *App) { a.toggleZoom() }, false},
		{"⇄ flip", func(a *App) { a.rotateFocusedSplit() }, false},
		{"≡ equal", func(a *App) { a.evenOutSplits() }, false},
	}

	y := a.area.H - 1
	out := make([]button, 0, len(items)+1)

	// The right-hand block is reserved before anything is laid out on the
	// left. Quit has to stay reachable however many buttons are added, and a
	// button drawn half over its neighbour is worse than a button absent.
	const quit = "⏻ quit"
	quitW := ansi.StringWidth(quit) + 2
	quitX := a.area.W - quitW - 1

	// Space is reserved for the widest label this can ever hold, not for the
	// one showing now. The label changes with session state, which does not
	// disturb the layout — so reserving the current width would let a longer
	// one grow into a button.
	a.barCountW = ansi.StringWidth("99 waiting")
	a.barCountX = quitX - 2 - a.barCountW
	limit := a.barCountX - 1

	x := 1
	for _, it := range items {
		w := ansi.StringWidth(it.label) + 2
		if x+w > limit {
			// Out of room. Everything after this is dropped rather than
			// squeezed: the keyboard and the help panel still reach it.
			break
		}
		out = append(out, button{
			label: it.label,
			rect:  layout.Rect{X: x, Y: y, W: w, H: 1},
			run:   it.run,
			warn:  it.warn,
		})
		x += w + 1
	}

	if quitX > x {
		out = append(out, button{
			label: quit,
			rect:  layout.Rect{X: quitX, Y: y, W: quitW, H: 1},
			run:   func(a *App) { a.overlay = overlayQuit },
			warn:  true,
		})
	}
	return out
}

// countLabel is what sits between the buttons and quit. A session waiting on
// you displaces the pane count: it is the one fact worth the space, and it is
// why the sessions module exists at all.
func (a *App) countLabel() string {
	if n := a.pool.Waiting(); n > 0 {
		return fmt.Sprintf("%d waiting", n)
	}
	if len(a.rects) == 1 {
		return "1 pane"
	}
	return fmt.Sprintf("%d panes", len(a.rects))
}

// drawStatusBar paints the bar and its buttons.
func (a *App) drawStatusBar(scr uv.Screen) {
	y := a.area.H - 1
	render.Fill(scr, uv.Rect(0, y, a.area.W, 1), barBg)

	for i, b := range a.buttons {
		fg, bg := color.Color(barFg), color.Color(barBg)
		if b.warn {
			fg = barQuitFg
		}
		if i == a.hoverBtn {
			fg, bg = barHotFg, barHotBg
		}
		render.Text(scr, b.rect.X, y, " "+b.label+" ", fg, bg)
	}

	// Pane count, right of the buttons and left of quit.
	// Computed here rather than at layout time: a hook changes what this says
	// without changing the shape of anything, and a label settled during
	// layout would never notice.
	if label := a.countLabel(); a.barCountX > 0 {
		x := a.barCountX + a.barCountW - ansi.StringWidth(label)
		render.Text(scr, x, y, label, barCountFg, barBg)
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
