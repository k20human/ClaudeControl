package app

import (
	"fmt"
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
	"claudecontrol/internal/transcript"
)

// titleH is the row every pane gives up to its title.
const titleH = 1

var (
	titleBg       = color.RGBA{R: 0x1b, G: 0x21, B: 0x2b, A: 0xff}
	titleFg       = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	titleFocusBg  = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	titleFocusFg  = color.RGBA{R: 0x10, G: 0x16, B: 0x1e, A: 0xff}
	titleDetailFg = color.RGBA{R: 0x9a, G: 0xa8, B: 0xbd, A: 0xff}
)

// contentRect is the part of a pane that belongs to its module. The top row is
// the title, so every module is handed a rectangle one row shorter than the
// pane — including for Resize, or the guest would draw a line the pane cannot
// show.
func contentRect(r layout.Rect) layout.Rect {
	if r.H <= titleH {
		return layout.Rect{X: r.X, Y: r.Y, W: r.W}
	}
	return layout.Rect{X: r.X, Y: r.Y + titleH, W: r.W, H: r.H - titleH}
}

// drawPaneTitles paints one title row per pane.
//
// The focused pane's title is filled in the accent colour rather than merely
// tinted: which pane takes your keystrokes is the one thing about this screen
// you should never have to look for.
func (a *App) drawPaneTitles(scr uv.Screen) {
	for id, r := range a.rects {
		if r.H < titleH || r.W <= 0 {
			continue
		}
		fg, bg := color.Color(titleFg), color.Color(titleBg)
		detail := color.Color(titleDetailFg)
		if id == a.focus {
			fg, bg, detail = titleFocusFg, titleFocusBg, titleFocusFg
		}
		render.Fill(scr, uv.Rect(r.X, r.Y, r.W, titleH), bg)

		name := a.moduleName(id)
		render.Text(scr, r.X+1, r.Y, truncate(name, r.W-2), fg, bg)
		used := ansi.StringWidth(name) + 2

		if rest := a.paneDetail(id); rest != "" && r.W-used > 3 {
			render.Text(scr, r.X+used, r.Y, truncate(" "+rest, r.W-used-1), detail, bg)
		}
	}
}

// paneDetail is what this pane is worth saying beyond its name: for a Claude
// session, the model it answered with and what that turn carried.
func (a *App) paneDetail(id layout.PaneID) string {
	sm, ok := a.modules[id].(interface{ SessionID() string })
	if !ok {
		return ""
	}
	m, ok := a.sessionUsage(sm.SessionID())
	if !ok {
		return ""
	}
	parts := make([]string, 0, 3)
	if m.Model != "" {
		parts = append(parts, shortModel(m.Model))
	}
	if m.Context > 0 {
		// Absolute counts, never a percentage: the context window is a
		// published number that goes out of date, and a wrong percentage is
		// worse than none.
		parts = append(parts, humanTokens(m.Context)+" ctx")
	}
	if m.CacheRate > 0 {
		parts = append(parts, fmt.Sprintf("%.0f%% cache", m.CacheRate*100))
	}
	return strings.Join(parts, " · ")
}

// sessionUsage is the last turn reported for a session.
func (a *App) sessionUsage(id string) (transcript.Metrics, bool) {
	a.usageMu.RLock()
	defer a.usageMu.RUnlock()
	m, ok := a.usage[id]
	return m, ok
}

// shortModel drops the vendor prefix, which is the same on every line and
// tells you nothing.
func shortModel(model string) string {
	return strings.TrimPrefix(model, "claude-")
}

// humanTokens keeps a token count to a few characters without lying about its
// order of magnitude.
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// truncate clips text to a width, marking that it was clipped.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string([]rune(s)[:w-1]) + "…"
}
