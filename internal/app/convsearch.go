package app

import (
	"fmt"
	"image/color"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
	"claudecontrol/internal/transcript"
)

var (
	convBg      = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	convQueryBg = color.RGBA{R: 0x1b, G: 0x24, B: 0x30, A: 0xff}
	convRowBg   = color.RGBA{R: 0x1e, G: 0x2a, B: 0x38, A: 0xff}
	convAccent  = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	convFg      = color.RGBA{R: 0x9a, G: 0xa8, B: 0xbd, A: 0xff}
	convDim     = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
)

// convLimit is how many conversations are listed. Beyond that you are better
// off narrowing the query than scrolling.
const convLimit = 40

// convSearch is the open search across the conversations on disk.
//
// The search runs off the draw loop because it reads every transcript — a
// couple of hundred milliseconds over four hundred megabytes — and an
// interface that stopped for that on every keystroke would be worse than one
// that made you press a key.
type convSearch struct {
	selected int
	top      int // the first result drawn, so a long list can be walked

	// The query is read by the goroutine that runs the search and written by
	// the one that reads the keyboard, so it lives under the lock with the
	// results rather than beside them.
	mu      sync.Mutex
	query   string
	hits    []transcript.Hit
	shown   string // the query the results belong to
	running bool
	took    time.Duration
}

// typed appends to the query.
func (c *convSearch) typed(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.query += text
}

// erased removes the last character of the query and reports whether there
// was one.
func (c *convSearch) erased() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.query == "" {
		return false
	}
	// A rune at a time, not a byte: one backspace on é must remove é rather
	// than half of it.
	_, n := utf8.DecodeLastRuneInString(c.query)
	c.query = c.query[:len(c.query)-n]
	return true
}

func (c *convSearch) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.query
}

// toggleConvSearch opens the search over the conversations, or closes it.
func (a *App) toggleConvSearch() {
	if a.conv != nil {
		a.closeConvSearch()
		return
	}
	a.conv = &convSearch{}
	a.Wake()
}

func (a *App) closeConvSearch() {
	if a.conv == nil {
		return
	}
	a.conv = nil
	a.clearNext = true
	a.Wake()
}

// runConvSearch keeps one search in flight at a time and starts another if the
// query moved on while it ran. Typing quickly therefore costs one search per
// pause rather than one per letter.
func (a *App) runConvSearch() {
	c := a.conv
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.running || c.query == "" {
		c.mu.Unlock()
		return
	}
	c.running = true
	query := c.query
	c.mu.Unlock()

	go func() {
		start := time.Now()
		hits := transcript.Search(query, convLimit)
		took := time.Since(start)

		c.mu.Lock()
		c.hits, c.shown, c.took, c.running = hits, query, took, false
		again := c.query != query
		c.mu.Unlock()

		a.Wake()
		if again {
			a.runConvSearch()
		}
	}()
}

// convResults is what is on screen, and whether it is still for the query you
// are looking at.
func (c *convSearch) results() ([]transcript.Hit, bool, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.shown == c.query, c.took
}

// convKey drives the panel and reports whether it took the key.
func (a *App) convKey(e uv.KeyPressEvent) bool {
	if a.conv == nil {
		return false
	}
	key := e.Key()
	switch {
	case key.Code == uv.KeyEscape, e.MatchString("alt+:"):
		a.closeConvSearch()
	case key.Code == uv.KeyEnter:
		a.openConversation()
	case key.Code == uv.KeyUp:
		a.moveConv(-1)
	case key.Code == uv.KeyDown:
		a.moveConv(1)
	case key.Code == uv.KeyBackspace:
		if a.conv.erased() {
			a.conv.selected, a.conv.top = 0, 0
			a.runConvSearch()
		}
	case key.Text != "":
		a.conv.typed(key.Text)
		a.conv.selected, a.conv.top = 0, 0
		a.runConvSearch()
	}
	return true
}

// scrollConv moves the selection without wrapping, which is what a wheel
// means: rolling past the first result should stop there, not leap to the
// last.
func (a *App) scrollConv(delta int) {
	hits, _, _ := a.conv.results()
	if len(hits) == 0 {
		return
	}
	next := a.conv.selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(hits) {
		next = len(hits) - 1
	}
	a.conv.selected = next
	a.Wake()
}

func (a *App) moveConv(delta int) {
	hits, _, _ := a.conv.results()
	if len(hits) == 0 {
		return
	}
	a.conv.selected += delta
	if a.conv.selected < 0 {
		a.conv.selected = len(hits) - 1
	}
	if a.conv.selected >= len(hits) {
		a.conv.selected = 0
	}
	a.Wake()
}

// openConversation resumes the highlighted one in a new tab of the focused
// pane, leaving what you were looking at where it was.
func (a *App) openConversation() {
	hits, _, _ := a.conv.results()
	if a.conv.selected < 0 || a.conv.selected >= len(hits) {
		return
	}
	hit := hits[a.conv.selected]
	a.closeConvSearch()

	m, ok := a.modules[a.focus]
	if !ok {
		return
	}
	adder, ok := m.(interface {
		Add(string, map[string]any) error
	})
	if !ok {
		a.setStatus("this pane has no tabs — open one in a pane of tabs")
		return
	}
	opts := map[string]any{"resume": hit.SessionID}
	if hit.Dir != "" {
		opts["dir"] = hit.Dir
	}
	if err := adder.Add("claude", opts); err != nil {
		a.setStatus("%s", err)
	}
}

// drawConvSearch paints the panel.
func (a *App) drawConvSearch(scr uv.Screen) {
	if a.conv == nil {
		return
	}
	r := a.convPanelRect()
	render.Fill(scr, uv.Rect(r.X, r.Y, r.W, r.H), convBg)
	render.Text(scr, r.X+2, r.Y, "find in conversations", convDim, convBg)

	query := a.conv.text()
	render.Fill(scr, uv.Rect(r.X, r.Y+1, r.W, 1), convQueryBg)
	render.Text(scr, r.X+2, r.Y+1, "› "+query+"▌", convAccent, convQueryBg)

	hits, fresh, took := a.conv.results()
	count := ""
	switch {
	case query == "":
		count = "type to search"
	case !fresh:
		count = "…"
	case len(hits) == 0:
		count = "none"
	default:
		count = fmt.Sprintf("%d in %dms", len(hits), took.Milliseconds())
	}
	if at := r.X + r.W - ansi.StringWidth(count) - 2; at > r.X+4 {
		render.Text(scr, at, r.Y+1, count, convDim, convQueryBg)
	}

	// Three rows a conversation: what it was called, where it ran, and a line
	// of what was actually said. The last one is what tells you whether it is
	// the conversation you meant.
	const rowsPer = 3
	room := (r.H - 4) / rowsPer
	if room < 1 {
		room = 1
	}
	// Keep the highlighted result on screen: walking off the bottom of a list
	// that never moves would leave nothing highlighted at all.
	if a.conv.selected < a.conv.top {
		a.conv.top = a.conv.selected
	}
	if a.conv.selected >= a.conv.top+room {
		a.conv.top = a.conv.selected - room + 1
	}
	if a.conv.top > len(hits)-room {
		a.conv.top = len(hits) - room
	}
	if a.conv.top < 0 {
		a.conv.top = 0
	}
	shown := hits[min(a.conv.top, len(hits)):]
	if len(shown) > room {
		shown = shown[:room]
	}
	for i, h := range shown {
		y := r.Y + 3 + i*rowsPer
		if y+2 >= r.Y+r.H {
			break
		}
		bg := color.Color(convBg)
		if a.conv.top+i == a.conv.selected {
			bg = convRowBg
			for k := 0; k < rowsPer; k++ {
				render.Fill(scr, uv.Rect(r.X, y+k, r.W, 1), bg)
			}
		}
		title := h.Title
		if title == "" {
			title = h.SessionID
		}
		render.Text(scr, r.X+2, y, clipBar(title, r.W-4), convAccent, bg)

		where := fmt.Sprintf("%s · %s · %d", shortDir(h.Dir), when(h.Modified), h.Matches)
		render.Text(scr, r.X+2, y+1, clipBar(where, r.W-4), convDim, bg)
		render.Text(scr, r.X+2, y+2, clipBar(h.Excerpt, r.W-4), convFg, bg)
	}

	if hidden := len(hits) - a.conv.top - len(shown); hidden > 0 {
		render.Text(scr, r.X+2, r.Y+r.H-1,
			fmt.Sprintf("%d more below   ⏎ open in a tab", hidden), convDim, convBg)
	} else if len(hits) > 0 {
		render.Text(scr, r.X+2, r.Y+r.H-1, "⏎ open in a tab   esc close", convDim, convBg)
	}
}

func (a *App) convPanelRect() layout.Rect {
	avail := a.paneArea()
	w, h := 78, 22
	if w > avail.W-4 {
		w = avail.W - 4
	}
	if h > avail.H-2 {
		h = avail.H - 2
	}
	if w < 24 {
		w = avail.W
	}
	if h < 6 {
		h = avail.H
	}
	return layout.Rect{
		X: avail.X + (avail.W-w)/2,
		Y: avail.Y + (avail.H-h)/2,
		W: w, H: h,
	}
}

// shortDir puts the home directory back as a tilde, which is how you think of
// it and half the width.
func shortDir(dir string) string {
	if dir == "" {
		return "?"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && len(dir) >= len(home) && dir[:len(home)] == home {
		return "~" + dir[len(home):]
	}
	return dir
}

// when says how long ago in the coarsest unit that is still true.
func when(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
