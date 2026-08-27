package app

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/indicator"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/render"
	"claudecontrol/internal/session"
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

// paneTitleH is how many rows this pane gives up to its title. A module that
// wants no title keeps the whole pane.
func (a *App) paneTitleH(id layout.PaneID) int {
	if t, ok := a.modules[id].(module.Titler); ok {
		if _, want := t.Title(); !want {
			return 0
		}
	}
	return titleH
}

// contentRect is the part of a pane that belongs to its module — including for
// Resize, or the guest would draw a line the pane cannot show.
func (a *App) contentRect(id layout.PaneID) layout.Rect {
	return shrinkTop(a.rects[id], a.paneTitleH(id))
}

// shrinkTop takes n rows off the top of a rectangle, never below nothing.
func shrinkTop(r layout.Rect, n int) layout.Rect {
	if n <= 0 {
		return r
	}
	if r.H <= n {
		return layout.Rect{X: r.X, Y: r.Y, W: r.W}
	}
	return layout.Rect{X: r.X, Y: r.Y + n, W: r.W, H: r.H - n}
}

// paneName is what to call a pane. The name Claude Code gave the session wins
// over anything else: it is the one a person recognises.
func (a *App) paneName(id layout.PaneID) string {
	if sm, ok := a.modules[id].(interface{ SessionID() string }); ok {
		if n := a.sessionName(sm.SessionID()); n != "" {
			return n
		}
	}
	if t, ok := a.modules[id].(module.Titler); ok {
		if name, want := t.Title(); want && name != "" {
			return name
		}
	}
	return a.moduleName(id)
}

// paneState is what the session behind a pane is doing, if it has one.
func (a *App) paneState(id layout.PaneID) (pool.State, bool) {
	sm, ok := a.modules[id].(interface{ SessionID() string })
	if !ok || a.pool == nil {
		return 0, false
	}
	e, ok := a.pool.Get(session.ID(sm.SessionID()))
	if !ok {
		return 0, false
	}
	return e.State, true
}

// sessionName is the name reported for a session, if one has been.
func (a *App) sessionName(id string) string {
	a.usageMu.RLock()
	defer a.usageMu.RUnlock()
	return a.names[id]
}

// drawPaneTitles paints one title row per pane.
//
// The focused pane's title is filled in the accent colour rather than merely
// tinted: which pane takes your keystrokes is the one thing about this screen
// you should never have to look for.
func (a *App) drawPaneTitles(scr uv.Screen) {
	for id, r := range a.rects {
		if r.H < titleH || r.W <= 0 || a.paneTitleH(id) == 0 {
			continue
		}
		fg, bg := color.Color(titleFg), color.Color(titleBg)
		detail := color.Color(titleDetailFg)
		if id == a.focus {
			fg, bg, detail = titleFocusFg, titleFocusBg, titleFocusFg
		}
		render.Fill(scr, uv.Rect(r.X, r.Y, r.W, titleH), bg)

		x := r.X + 1
		if st, ok := a.paneState(id); ok {
			// The mark first, because it is what a glance is looking for.
			mark := indicator.Glyph(st, time.Now())
			markFg := color.Color(indicator.Colour(st))
			if id == a.focus {
				// On the filled title the state colours would fight the
				// accent, so the mark takes the title's own foreground and
				// says what it says by shape alone.
				markFg = fg
			}
			render.Text(scr, x, r.Y, mark, markFg, bg)
			x += ansi.StringWidth(mark) + 1
		}

		name := a.paneName(id)
		render.Text(scr, x, r.Y, truncate(name, r.X+r.W-1-x), fg, bg)
		used := x - r.X + ansi.StringWidth(name) + 1

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
		parts = append(parts, transcript.ShortModel(m.Model))
	}
	if m.Context > 0 {
		// Absolute counts, never a percentage: the context window is a
		// published number that goes out of date, and a wrong percentage is
		// worse than none.
		parts = append(parts, transcript.HumanTokens(m.Context)+" ctx")
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
