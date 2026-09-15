package app

import (
	"fmt"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
)

// overlayKind is what, if anything, is shown on top of the panes.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayHelp
	overlayQuit
	// overlayClosing asks what to do with the conversation a tab or pane
	// holds, before closing it.
	overlayClosing
)

var (
	panelBg   = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	panelFg   = color.RGBA{R: 0xc7, G: 0xd2, B: 0xe0, A: 0xff}
	panelKey  = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	panelWarn = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
	btnBg     = color.RGBA{R: 0x24, G: 0x2e, B: 0x3d, A: 0xff}
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

// drawOverlay paints the help panel or the quit confirmation, and records the
// buttons a click can land on.
func (a *App) drawOverlay(scr uv.Screen) {
	a.panelButtons = nil
	switch a.overlay {
	case overlayHelp:
		a.drawPanel(scr, "SHORTCUTS", helpLines(), "or press any key", panelFg, []panelAction{
			{"[ close ]", false, func(a *App) { a.dismissOverlay(false) }},
		})
	case overlayClosing:
		a.drawPanel(scr, "CLOSE", a.closingLines(),
			"enter closes it — x closes and ends it — any other key stays",
			panelFg, []panelAction{
				{"[ close ]", false, func(a *App) { a.finishClose(false) }},
				{"[ end it ]", true, func(a *App) { a.finishClose(true) }},
				{"[ cancel ]", false, func(a *App) { a.cancelClose() }},
			})
	case overlayQuit:
		n := len(a.pool.All())
		what := fmt.Sprintf("Quit and end %d sessions?", n)
		if n == 1 {
			what = "Quit and end 1 session?"
		}
		a.drawPanel(scr, "QUIT", [][2]string{
			{"", what},
			{"", "Sessions do not survive the application."},
			{"", ""},
			{"", "The arrangement you leave — panes, tabs, dividers —"},
			{"", "comes back next time unless you clear the box."},
		}, "y or enter to quit — space toggles the box — any other key to stay",
			panelWarn, []panelAction{
				{a.keepBoxLabel(), false, func(a *App) { a.keepLayout = !a.keepLayout }},
				{"[ quit ]", true, func(a *App) { a.dismissOverlay(true) }},
				{"[ stay ]", false, func(a *App) { a.dismissOverlay(false) }},
			})
	}
}

// panelAction is a clickable label at the foot of a panel.
type panelAction struct {
	label string
	warn  bool
	run   func(a *App)
}

// drawPanel centres a box over the panes.
//
// It never encroaches on the status bar, and it drops the rows that do not fit
// rather than painting outside itself: an overlay that spills is worse than an
// overlay that is short, because what it spills onto is a live session.
func (a *App) drawPanel(scr uv.Screen, title string, rows [][2]string, footer string, accent color.Color, actions []panelAction) {
	avail := a.paneArea()

	// Key column first: rows are drawn aligned on the longest key, so a row is
	// as wide as that column plus its own text, never as its own key plus its
	// text. Measuring the latter under-reports every short-key row and the
	// panel loses its right-hand margin.
	keyW := 0
	for _, r := range rows {
		if w := ansi.StringWidth(r[0]); w > keyW {
			keyW = w
		}
	}
	textW := ansi.StringWidth(title)
	for _, r := range rows {
		w := ansi.StringWidth(r[1])
		if r[0] != "" {
			w += keyW + 2
		}
		if w > textW {
			textW = w
		}
	}
	if w := ansi.StringWidth(footer); w > textW {
		textW = w
	}
	actionsW := 0
	for i, act := range actions {
		if i > 0 {
			actionsW += 2
		}
		actionsW += ansi.StringWidth(act.label)
	}
	if actionsW > textW {
		textW = actionsW
	}

	const padX = 3
	chrome := 6 // title, blank line, footer and margins
	if len(actions) > 0 {
		chrome += 2 // the button row and the blank line above it
	}

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
	render.Fill(scr, uv.Rect(x, y, w, h), panelBg)
	render.Text(scr, x+padX, y+1, title, accent, panelBg)

	for i, r := range shown {
		if r[0] == "" && r[1] == "" {
			continue
		}
		if r[0] == "" {
			// A line with no key is a sentence, not a table row: it starts at
			// the margin instead of hanging off an empty key column.
			render.Text(scr, x+padX, y+3+i, r[1], panelFg, panelBg)
			continue
		}
		render.Text(scr, x+padX, y+3+i, r[0], panelKey, panelBg)
		render.Text(scr, x+padX+keyW+2, y+3+i, r[1], panelFg, panelBg)
	}

	if len(actions) > 0 && h >= chrome {
		by := y + h - 3
		bx := x + padX
		for _, act := range actions {
			lw := ansi.StringWidth(act.label)
			fg := color.Color(panelFg)
			if act.warn {
				fg = panelWarn
			}
			bg := color.Color(btnBg)
			r := layout.Rect{X: bx, Y: by, W: lw, H: 1}
			// Tested against the rectangle rather than looked up in
			// panelButtons: that slice is still being built, and at this point
			// it does not yet hold the button being drawn.
			if a.pointerY == by && a.pointerX >= bx && a.pointerX < bx+lw {
				fg, bg = barHotFg, barHotBg
			}
			render.Text(scr, bx, by, act.label, fg, bg)
			a.panelButtons = append(a.panelButtons, button{
				label: act.label, rect: r, run: act.run, warn: act.warn,
			})
			bx += lw + 2
		}
	}
	if h >= chrome {
		render.Text(scr, x+padX, y+h-2, footer, barCountFg, panelBg)
	}
}

// dismissOverlay closes the overlay, acting on the answer when one is wanted.
// It reports whether the input was consumed, which it always is: closing a
// panel must never leak the keystroke into a session underneath.
func (a *App) dismissOverlay(confirmed bool) bool {
	if a.overlay == overlayQuit && confirmed {
		// Written here rather than on the way out: the panes and their
		// modules are still there to be asked what they hold.
		a.saveLayout()
		a.quit = true
	}
	a.overlay = overlayNone
	a.panelButtons = nil
	a.clearNext = true
	return true
}

// keepBoxLabel is the tick as it reads in the panel.
func (a *App) keepBoxLabel() string {
	if a.keepLayout {
		return "[x] save this layout"
	}
	return "[ ] save this layout"
}
