package app

import (
	"fmt"
	"image/color"
	"math"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
	"claudecontrol/internal/usage"
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
		{"⌘ cmd", func(a *App) { a.togglePalette() }, false},
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

	// The account budget sits beside the count, and only when something is
	// already fetching it: reserving the space unconditionally would shorten
	// the bar for everyone who never asked for the figure.
	//
	// Reserved on the presence of a source, not on a reading: the first
	// reading arrives seconds after the layout is settled, and a slot that
	// appeared then would shove the buttons sideways under the pointer.
	//
	// The widest shape that still leaves room for the buttons that matter
	// wins, so the figure never costs you the way back to the rest of the
	// interface.
	a.barUsageX, a.barUsageW, a.barUsageForm = 0, 0, 0
	if a.hasAccountSource() {
		essential := 1
		for _, it := range items[:min(len(items), essentialButtons)] {
			essential += ansi.StringWidth(it.label) + 3
		}
		for form, sample := range usageForms {
			w := ansi.StringWidth(sample)
			if x := a.barCountX - 2 - w; x >= essential {
				a.barUsageX, a.barUsageW, a.barUsageForm = x, w, form
				limit = x - 1
				break
			}
		}
	}

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
// statusHold is how long a message stays before the buttons come back. Long
// enough to read a sentence, short enough that the bar is not a log.
const statusHold = 5 * time.Second

// showingStatus reports whether a message is currently taking the bar. While
// it is, the buttons it covers are neither drawn nor clickable: a button you
// cannot see must not be a button you can press.
func (a *App) showingStatus() bool {
	return a.status != "" && time.Since(a.statusAt) < statusHold
}

// setStatus puts a sentence in the bar. It is how the application answers
// something it was asked to do but could not.
func (a *App) setStatus(format string, args ...any) {
	a.status = fmt.Sprintf(format, args...)
	a.statusAt = time.Now()
	a.Wake()
}

func (a *App) drawStatusBar(scr uv.Screen) {
	y := a.area.H - 1
	render.Fill(scr, uv.Rect(0, y, a.area.W, 1), barBg)

	if a.showingStatus() {
		limit := a.barCountX
		if a.barUsageX > 0 {
			limit = a.barUsageX
		}
		render.Text(scr, 1, y, clipBar(a.status, limit-2), barHotBg, barBg)
	}

	for i, b := range a.buttons {
		if a.showingStatus() && !b.warn {
			// Quit stays: it is the way out, and the way out is never hidden
			// behind a message.
			continue
		}
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

	a.drawBarUsage(scr, y)
}

// drawBarUsage reprises the account's budgets in the bar, so the figure is
// there without opening the stats pane.
//
// Only what a glance needs: the two shares. Reset times and per-model caps
// stay in the pane, which has the room to say what they mean.
func (a *App) drawBarUsage(scr uv.Screen, y int) {
	if a.barUsageX <= 0 {
		return
	}
	r, ok := a.accountReading()
	if !ok {
		return
	}
	if r.Err != nil {
		render.Text(scr, a.barUsageX, y, "usage unavailable", barQuitFg, barBg)
		return
	}
	label := a.usageLabel(r, time.Now())
	x := a.barUsageX + a.barUsageW - ansi.StringWidth(label)
	fg := barCountFg
	if worst := math.Max(r.Snapshot.FiveHour.Percent, r.Snapshot.SevenDay.Percent); worst >= 85 {
		fg = barQuitFg
	}
	render.Text(scr, x, y, label, fg, barBg)
}

// essentialButtons is how many of the status bar's buttons the budget figure
// must never push off the bar.
const essentialButtons = 4

// usageForms are the shapes the budget reprise can take, widest first. They
// are the widest text each shape can ever hold, not the text showing now: the
// share and the countdown both change without the layout changing, and
// reserving the current width would let a longer one grow into a button.
var usageForms = []string{
	"usage 5h 100% ↻ 99h59m · 7d 100% ↻ 9d23h",
	"usage 5h 100% ↻ 99h59m · 7d 100%",
	"usage 5h 100% · 7d 100%",
	// Last resort: the five-hour window alone. It is the one that runs out
	// first, and the word stays because a bare "5h 42%" says nothing about
	// what is at 42%.
	"usage 5h 100%",
}

// usageLabel writes the budgets in whichever shape was reserved. It is named
// because a bare "5h 42%" says nothing about what is at 42%.
func (a *App) usageLabel(r usage.Reading, now time.Time) string {
	five := fmt.Sprintf("5h %.0f%%", r.Snapshot.FiveHour.Percent)
	seven := fmt.Sprintf("7d %.0f%%", r.Snapshot.SevenDay.Percent)
	if a.barUsageForm <= 1 && r.Snapshot.FiveHour.Resets {
		five += " ↻ " + usage.Until(r.Snapshot.FiveHour.ResetsAt, now)
	}
	if a.barUsageForm >= 3 {
		return "usage " + five
	}
	if a.barUsageForm == 0 && r.Snapshot.SevenDay.Resets {
		seven += " ↻ " + usage.Until(r.Snapshot.SevenDay.ResetsAt, now)
	}
	return "usage " + five + " · " + seven
}

// hasAccountSource reports whether any pane is fetching the account's budgets.
func (a *App) hasAccountSource() bool {
	for _, m := range a.modules {
		if _, ok := m.(interface{ Account() (usage.Reading, bool) }); ok {
			return true
		}
	}
	return false
}

// accountReading is the last budget reading from whichever pane is fetching
// one. Nothing here starts a fetch: the bar reports, it does not ask.
func (a *App) accountReading() (usage.Reading, bool) {
	for _, m := range a.modules {
		acc, ok := m.(interface{ Account() (usage.Reading, bool) })
		if !ok {
			continue
		}
		if r, taken := acc.Account(); taken {
			return r, true
		}
	}
	return usage.Reading{}, false
}

// buttonAt returns the index of the button under the pointer, or -1.
// clipBar shortens a message to the room the bar has for it.
func clipBar(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// buttonUnder is what is clickable at a point, which is only ever what is
// drawn there.
func (a *App) buttonUnder(x, y int) int {
	i := buttonAt(a.buttons, x, y)
	if i >= 0 && a.showingStatus() && !a.buttons[i].warn {
		return -1
	}
	return i
}

func buttonAt(buttons []button, x, y int) int {
	for i, b := range buttons {
		if x >= b.rect.X && x < b.rect.X+b.rect.W && y >= b.rect.Y && y < b.rect.Y+b.rect.H {
			return i
		}
	}
	return -1
}
