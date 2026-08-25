package app

import (
	"fmt"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
)

// overlayKind is what, if anything, is shown on top of the panes.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayHelp
	overlayQuit
)

var (
	panelBg   = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	panelFg   = color.RGBA{R: 0xc7, G: 0xd2, B: 0xe0, A: 0xff}
	panelKey  = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	panelWarn = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
)

// helpLines builds the shortcut list from the binding table itself, so a
// binding added without a line here is impossible: the panel cannot fall out
// of step with what the application actually does.
func helpLines() [][2]string {
	out := make([][2]string, 0, len(bindings)+4)
	for _, b := range bindings {
		if b.desc == "" {
			continue
		}
		out = append(out, [2]string{b.display, b.desc})
	}
	out = append(out,
		[2]string{"", ""},
		[2]string{"click", "focus a pane"},
		[2]string{"click again", "send the click to the guest"},
		[2]string{"drag a divider", "resize"},
	)
	return out
}

// drawOverlay paints the help panel or the quit confirmation.
func (a *App) drawOverlay(scr uv.Screen) {
	switch a.overlay {
	case overlayHelp:
		a.drawPanel(scr, "SHORTCUTS", helpLines(), "press any key or click to close", panelFg)
	case overlayQuit:
		n := len(a.sessions.All())
		what := fmt.Sprintf("Quit and end %d sessions?", n)
		if n == 1 {
			what = "Quit and end 1 session?"
		}
		a.drawPanel(scr, "QUIT", [][2]string{
			{"", what},
			{"", ""},
			{"y / enter", "quit"},
			{"any other key", "stay"},
		}, "sessions do not survive the application", panelWarn)
	}
}

// drawPanel centres a box over the panes.
//
// It never encroaches on the status bar, and it drops the rows that do not fit
// rather than painting outside itself: an overlay that spills is worse than an
// overlay that is short, because what it spills onto is a live session.
func (a *App) drawPanel(scr uv.Screen, title string, rows [][2]string, footer string, accent color.Color) {
	avail := a.paneArea()

	// Key column first: the rows are drawn aligned on the longest key, so the
	// width of a row is that column plus its text, never its own key plus its
	// text. Measuring the latter under-reports every row with a short key and
	// a long description, and the panel loses its right-hand margin.
	keyW := 0
	for _, r := range rows {
		if w := ansi.StringWidth(r[0]); w > keyW {
			keyW = w
		}
	}
	textW := ansi.StringWidth(title)
	for _, r := range rows {
		if w := keyW + 2 + ansi.StringWidth(r[1]); w > textW {
			textW = w
		}
	}
	if w := ansi.StringWidth(footer); w > textW {
		textW = w
	}

	const padX, chrome = 3, 6 // side padding; title, blank, footer and margins
	w := textW + 2*padX
	if w > avail.W {
		w = avail.W
	}
	shown := rows
	h := len(shown) + chrome
	if h > avail.H {
		h = avail.H
		if fit := h - chrome; fit < len(shown) {
			if fit < 0 {
				fit = 0
			}
			shown = shown[:fit]
		}
	}
	if len(shown) < len(rows) {
		footer = fmt.Sprintf("%d more — the window is too short", len(rows)-len(shown))
	}

	x := avail.X + (avail.W-w)/2
	y := avail.Y + (avail.H-h)/2
	fill(scr, layout.Rect{X: x, Y: y, W: w, H: h}, panelBg)
	writeText(scr, x+padX, y+1, title, accent, panelBg)
	for i, r := range shown {
		if r[0] == "" && r[1] == "" {
			continue
		}
		writeText(scr, x+padX, y+3+i, r[0], panelKey, panelBg)
		writeText(scr, x+padX+keyW+2, y+3+i, r[1], panelFg, panelBg)
	}
	if h >= chrome {
		writeText(scr, x+padX, y+h-2, footer, barCountFg, panelBg)
	}
}

// dismissOverlay closes the overlay, acting on the answer when one is wanted.
// It reports whether the input was consumed, which it always is: closing a
// panel must never leak the keystroke into a session underneath.
func (a *App) dismissOverlay(confirmed bool) bool {
	if a.overlay == overlayQuit && confirmed {
		a.quit = true
	}
	a.overlay = overlayNone
	a.clearNext = true
	return true
}
