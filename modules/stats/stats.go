// Package stats shows what the sessions have spent and what the account has
// left.
//
// Two sources, deliberately kept apart. Per-session figures come from the
// transcripts on disk and cost nothing. The account's five-hour and weekly
// budgets come from an endpoint reached with the OAuth token Claude Code
// stores — so that request is made only because this module was configured,
// and it can be switched off with `account: false`.
package stats

import (
	"fmt"
	"image/color"
	"sort"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/render"
	"claudecontrol/internal/session"
	"claudecontrol/internal/transcript"
	"claudecontrol/internal/usage"
)

func init() { module.Register("stats", New) }

var (
	bgPanel  = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	fgHead   = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	fgText   = color.RGBA{R: 0x9a, G: 0xa8, B: 0xbd, A: 0xff}
	fgMuted  = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	fgCalm   = color.RGBA{R: 0x6c, G: 0xc4, B: 0x8a, A: 0xff}
	fgWarm   = color.RGBA{R: 0xe0, G: 0xb0, B: 0x5c, A: 0xff}
	fgHot    = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
	fgBroken = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
)

// The bar takes whatever is left once the label, the share and the countdown
// have had their room: those carry the information, the bar only makes it
// glanceable. Below barMin there is nothing left to see and the bar is
// dropped entirely rather than drawn as two cells pretending to be a scale.
const (
	barMax = 16
	barMin = 4
)

// Module lists the sessions' token use and the account's budgets.
type Module struct {
	interval    time.Duration
	endpoint    string
	credentials string
	account     bool

	pool   *pool.Pool
	poller *usage.Poller

	mu      sync.RWMutex
	perSess map[string]transcript.Metrics
	reading usage.Reading
	hasRead bool
}

// New builds the module from its configuration.
func New(cfg map[string]any) (module.Module, error) {
	m := &Module{
		interval: usage.DefaultInterval,
		account:  true,
		perSess:  make(map[string]transcript.Metrics),
	}
	if v, ok := toFloat(cfg["interval"]); ok && v > 0 {
		m.interval = time.Duration(v * float64(time.Second))
	}
	if v, ok := cfg["endpoint"].(string); ok {
		m.endpoint = v
	}
	if v, ok := cfg["credentials"].(string); ok {
		m.credentials = v
	}
	if v, ok := cfg["account"].(bool); ok {
		m.account = v
	}
	return m, nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// Init follows the per-session metrics and, unless asked not to, starts asking
// for the account's budgets.
func (m *Module) Init(ctx module.Context) error {
	m.pool = ctx.Pool

	if ctx.Bus != nil {
		ch := ctx.Bus.SubscribeState(transcript.SessionTopic)
		go func() {
			for v := range ch {
				sm, ok := v.(transcript.SessionMetrics)
				if !ok {
					continue
				}
				m.mu.Lock()
				m.perSess[sm.SessionID] = sm.Metrics
				m.mu.Unlock()
				if ctx.Wake != nil {
					ctx.Wake()
				}
			}
		}()
	}

	if !m.account {
		return nil
	}
	m.poller = usage.NewPoller(
		&usage.Client{Endpoint: m.endpoint, CredentialsPath: m.credentials},
		nil, m.interval, ctx.Wake,
	)
	// Read from the poller rather than the bus: the reading is this module's
	// own, and publishing it would invite a second stats pane to display a
	// figure it never asked for.
	m.poller.Start()
	return nil
}

// Resize records nothing: the list draws to whatever area it is given.
func (m *Module) Resize(int, int) error { return nil }

// Close stops asking.
func (m *Module) Close() error {
	if m.poller != nil {
		m.poller.Stop()
	}
	return nil
}

// Account is the last budget reading, and whether one has been taken. The
// status bar reprises it; nothing about calling this starts a request.
func (m *Module) Account() (usage.Reading, bool) {
	if m.poller == nil {
		return usage.Reading{}, false
	}
	return m.poller.Last()
}

// Refresh asks for the account's budgets out of turn.
func (m *Module) Refresh() {
	if m.poller != nil {
		go m.poller.Refresh()
	}
}

// row is one line to paint, with the colour it earned.
type row struct {
	text string
	fg   color.Color
}

// Draw paints the panel.
func (m *Module) Draw(scr uv.Screen, area uv.Rectangle) {
	render.Fill(scr, area, bgPanel)
	w := area.Dx()
	if w <= 0 || area.Dy() <= 0 {
		return
	}
	y := area.Min.Y
	for _, r := range m.rows(w) {
		if y >= area.Max.Y {
			return
		}
		render.Text(scr, area.Min.X, y, clip(r.text, w), r.fg, bgPanel)
		y++
	}
}

// wideAt is the width below which labels are abbreviated. Above it there is
// room to say what a number means, which is worth more than the room it costs.
const wideAt = 46

// rows is everything the panel has to say, in order.
func (m *Module) rows(w int) []row {
	head := "plan usage"
	if w >= wideAt {
		head = "plan usage — share used, and when it refills"
	}
	out := []row{{head, fgHead}}
	out = append(out, m.accountRows(w)...)

	head = "sessions"
	if w >= wideAt {
		head = "sessions — tokens carried by the last turn"
	}
	out = append(out, row{"", fgText}, row{head, fgHead})
	out = append(out, m.sessionRows(w)...)
	return out
}

func (m *Module) accountRows(w int) []row {
	if !m.account {
		return []row{{"  not asked (account: false)", fgMuted}}
	}
	r, taken := m.poller.Last()
	if !taken {
		return []row{{"  asking…", fgMuted}}
	}
	if r.Err != nil {
		// The reason, not a blank line: an unavailable budget and an unused
		// one must never look the same.
		return []row{
			{"  unavailable", fgBroken},
			{"  " + clip(r.Err.Error(), max(w-2, 1)), fgMuted},
		}
	}
	rows := []row{
		budgetRow("5h", r.Snapshot.FiveHour, w),
		budgetRow("week", r.Snapshot.SevenDay, w),
	}
	for _, s := range r.Snapshot.Scoped {
		rows = append(rows, budgetRow("week "+s.Model, s.Window, w))
	}
	return rows
}

// budgetRow renders one budget: its share as a number, when it refills, and a
// bar filling whatever room is left.
func budgetRow(label string, win usage.Window, w int) row {
	labelW := 6
	if w >= wideAt {
		labelW = 11
	}
	head := fmt.Sprintf("  %-*s ", labelW, clip(label, labelW))
	tail := fmt.Sprintf(" %3.0f%%", win.Percent)
	if win.Resets {
		left := resetIn(win.ResetsAt, time.Now())
		if w >= wideAt {
			tail += "   refills in " + left
		} else {
			tail += "  ↻ " + left
		}
	}
	width := w - ansi.StringWidth(head) - ansi.StringWidth(tail)
	if width > barMax {
		width = barMax
	}
	if width < barMin {
		// No room for a scale: the number still says everything the bar would.
		return row{clip(fmt.Sprintf("  %s%s", clip(label, labelW), tail), w), budgetColour(win.Percent)}
	}
	return row{head + bar(win.Percent, width) + tail, budgetColour(win.Percent)}
}

// resetIn says how long is left rather than at what clock time, because a
// clock time has to be compared against another clock to mean anything.
func resetIn(at, now time.Time) string {
	d := at.Sub(now)
	if d <= 0 {
		return "now"
	}
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", max(int(d.Minutes()), 1))
}

func budgetColour(percent float64) color.Color {
	switch {
	case percent >= 85:
		return fgHot
	case percent >= 60:
		return fgWarm
	}
	return fgCalm
}

// bar draws a share as a filled strip of the given width.
func bar(percent float64, width int) string {
	filled := int(percent/100*float64(width) + 0.5)
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	out := make([]rune, 0, width)
	for i := 0; i < width; i++ {
		if i < filled {
			out = append(out, '█')
			continue
		}
		out = append(out, '░')
	}
	return string(out)
}

func (m *Module) sessionRows(w int) []row {
	if m.pool == nil {
		return []row{{"  no sessions", fgMuted}}
	}
	entries := m.pool.All()
	if len(entries) == 0 {
		return []row{{"  no sessions", fgMuted}}
	}
	m.mu.RLock()
	seen := make(map[string]transcript.Metrics, len(m.perSess))
	for k, v := range m.perSess {
		seen[k] = v
	}
	m.mu.RUnlock()

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Title < entries[j].Title })

	rows := make([]row, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, sessionRow(e, seen, w))
	}
	return rows
}

func sessionRow(e *pool.Entry, seen map[string]transcript.Metrics, w int) row {
	name := e.Title
	if name == "" && e.Session != nil {
		name = string(e.Session.ID)
	}
	line := fmt.Sprintf("  %-12s", clip(name, 12))
	var id session.ID
	if e.Session != nil {
		id = e.Session.ID
	}
	met, ok := seen[string(id)]
	if !ok {
		// A session with no assistant turn yet has nothing to report, which is
		// not the same as reporting zero.
		return row{line + "  — no turn yet", fgMuted}
	}
	ctx, cached, thinking := "ctx", "cached", "think"
	if w >= wideAt {
		ctx, cached, thinking = "context", "from cache", "thinking"
	}
	line += fmt.Sprintf("  %-9s %6s %s", clip(transcript.ShortModel(met.Model), 9),
		transcript.HumanTokens(met.Context), ctx)

	// The remaining figures are added only if they fit whole. A row ending in
	// "1.2k thinki…" teaches nothing that dropping it would not.
	if met.CacheRate > 0 {
		line = extend(line, fmt.Sprintf("  %.0f%% %s", met.CacheRate*100, cached), w)
	}
	if met.Thinking > 0 {
		line = extend(line, "  "+transcript.HumanTokens(met.Thinking)+" "+thinking, w)
	}
	return row{clip(line, w), fgText}
}

// extend appends a segment when the whole of it fits, and otherwise leaves the
// line as it was.
func extend(line, segment string, w int) string {
	if ansi.StringWidth(line)+ansi.StringWidth(segment) > w {
		return line
	}
	return line + segment
}

// clip shortens text to a width, marking that it was shortened.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}
