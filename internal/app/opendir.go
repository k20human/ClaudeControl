package app

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
	"claudecontrol/internal/transcript"
)

// Choosing where a session opens, with the pointer.
//
// A tab's directory is where its process is running, so there is no changing
// it — a conversation cannot be moved to another directory any more than a
// running program can. What there is instead is opening the next one
// somewhere: this panel picks the somewhere, and the session that is already
// running is left alone.
//
// The list starts as the directories you have actually worked in, newest
// first, because the one you want is nearly always among the last few. Typing
// filters it; typing something that looks like a path offers the filesystem
// too, so a directory you have never worked in is reachable — and offered as
// entries you can click rather than as completion you have to spell.

var (
	dirBg      = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	dirQueryBg = color.RGBA{R: 0x1b, G: 0x24, B: 0x30, A: 0xff}
	dirRowBg   = color.RGBA{R: 0x1e, G: 0x2a, B: 0x38, A: 0xff}
	dirAccent  = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	dirFg      = color.RGBA{R: 0x9a, G: 0xa8, B: 0xbd, A: 0xff}
	dirDim     = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
)

// dirLimit is how many of your directories are remembered. Beyond that you are
// better off typing the path than scrolling for it.
const dirLimit = 60

// dirChoice is one directory the panel offers.
type dirChoice struct {
	dir  string
	last time.Time
	// worked says this is somewhere you have had a conversation, rather than
	// somewhere the filesystem simply has.
	worked bool
}

// openDirState is the open panel.
type openDirState struct {
	query    string
	selected int
	top      int

	// places is gathered once when the panel opens: it costs a few dozen
	// small reads, and a list that changed under you while you typed would be
	// a list you could not click.
	places []dirChoice
}

// toggleOpenDir opens the panel, or closes it.
func (a *App) toggleOpenDir() {
	if a.openDir != nil {
		a.closeOpenDir()
		return
	}
	found := transcript.Places(dirLimit)
	places := make([]dirChoice, 0, len(found)+1)
	// Where this pane is working comes first: opening another session beside
	// the one you are looking at is the commonest thing to want.
	if here := a.focusedDir(); here != "" {
		places = append(places, dirChoice{dir: here, worked: true})
	}
	for _, p := range found {
		if len(places) > 0 && p.Dir == places[0].dir {
			places[0].last = p.Last
			continue
		}
		places = append(places, dirChoice{dir: p.Dir, last: p.Last, worked: true})
	}
	a.openDir = &openDirState{places: places}
	a.Wake()
}

func (a *App) closeOpenDir() {
	if a.openDir == nil {
		return
	}
	a.openDir = nil
	a.clearNext = true
	a.Wake()
}

// openDirChoices is what the panel is showing: your directories filtered by
// what you have typed, and the filesystem when what you have typed is a path.
func (a *App) openDirChoices() []dirChoice {
	d := a.openDir
	if d == nil {
		return nil
	}
	query := strings.TrimSpace(d.query)
	var out []dirChoice
	for _, p := range d.places {
		if query == "" || strings.Contains(strings.ToLower(p.dir), strings.ToLower(query)) {
			out = append(out, p)
		}
	}
	for _, dir := range browse(query) {
		if !alreadyOffered(out, dir) {
			out = append(out, dirChoice{dir: dir})
		}
	}
	return out
}

func alreadyOffered(in []dirChoice, dir string) bool {
	for _, c := range in {
		if c.dir == dir {
			return true
		}
	}
	return false
}

// browse is the filesystem's answer to a path being typed, and nothing at all
// to anything else: a query that is not a path is a filter over where you have
// worked, and reading directories for it would be answering a question nobody
// asked.
func browse(query string) []string {
	if query == "" {
		return nil
	}
	if !strings.HasPrefix(query, "/") && !strings.HasPrefix(query, "~") && !strings.HasPrefix(query, ".") {
		return nil
	}
	path := expandHome(query)

	dir, prefix := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	// The directory itself, when it is one and was typed whole.
	if st, err := os.Stat(path); err == nil && st.IsDir() && !alreadyIn(out, path) {
		out = append([]string{path}, out...)
	}
	if len(out) > dirLimit {
		out = out[:dirLimit]
	}
	return out
}

func alreadyIn(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}

// expandHome reads ~ the way a shell does.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// openDirKey drives the panel and reports whether it took the key.
func (a *App) openDirKey(e uv.KeyPressEvent) bool {
	if a.openDir == nil {
		return false
	}
	key := e.Key()
	switch {
	case key.Code == uv.KeyEscape:
		a.closeOpenDir()
	case key.Code == uv.KeyEnter:
		a.openChosenDir()
	case key.Code == uv.KeyUp:
		a.moveOpenDir(-1)
	case key.Code == uv.KeyDown:
		a.moveOpenDir(1)
	case key.Code == uv.KeyBackspace:
		if n := len(a.openDir.query); n > 0 {
			_, size := utf8.DecodeLastRuneInString(a.openDir.query)
			a.openDir.query = a.openDir.query[:n-size]
			a.openDir.selected, a.openDir.top = 0, 0
			a.Wake()
		}
	case key.Text != "":
		a.openDir.query += key.Text
		a.openDir.selected, a.openDir.top = 0, 0
		a.Wake()
	}
	return true
}

func (a *App) moveOpenDir(delta int) {
	choices := a.openDirChoices()
	if len(choices) == 0 {
		return
	}
	a.openDir.selected += delta
	if a.openDir.selected < 0 {
		a.openDir.selected = len(choices) - 1
	}
	if a.openDir.selected >= len(choices) {
		a.openDir.selected = 0
	}
	a.Wake()
}

// openChosenDir opens a session in the directory under the highlight.
//
// A new tab, always. The conversation already running in this pane is not
// disturbed: choosing where to work next is not a reason to end what you are
// doing now.
func (a *App) openChosenDir() {
	choices := a.openDirChoices()
	if a.openDir.selected < 0 || a.openDir.selected >= len(choices) {
		return
	}
	dir := choices[a.openDir.selected].dir
	a.closeOpenDir()
	a.openSessionIn(dir)
}

// openSessionIn puts a session in a directory: a tab where there are tabs, and
// a pane where there are not.
func (a *App) openSessionIn(dir string) {
	if adder, ok := a.modules[a.focus].(interface {
		Add(string, map[string]any) error
	}); ok {
		if err := adder.Add("claude", map[string]any{"dir": dir}); err != nil {
			a.setStatus("%s", err)
		}
		return
	}
	if err := a.newPaneIn(dir, layout.Horizontal); err != nil {
		a.setStatus("%s", err)
	}
}

// drawOpenDir paints the panel.
func (a *App) drawOpenDir(scr uv.Screen) {
	d := a.openDir
	if d == nil {
		return
	}
	r := a.convPanelRect()
	render.Fill(scr, uv.Rect(r.X, r.Y, r.W, r.H), dirBg)
	render.Text(scr, r.X+2, r.Y, "open a session in", dirDim, dirBg)

	render.Fill(scr, uv.Rect(r.X, r.Y+1, r.W, 1), dirQueryBg)
	render.Text(scr, r.X+2, r.Y+1, "› "+d.query+"▌", dirAccent, dirQueryBg)

	choices := a.openDirChoices()
	hint := "type a path for anywhere else"
	if len(choices) == 0 {
		hint = "no such directory"
	}
	if at := r.X + r.W - ansi.StringWidth(hint) - 2; at > r.X+4 {
		render.Text(scr, at, r.Y+1, hint, dirDim, dirQueryBg)
	}

	room := r.H - 4
	if room < 1 {
		room = 1
	}
	if d.selected < d.top {
		d.top = d.selected
	}
	if d.selected >= d.top+room {
		d.top = d.selected - room + 1
	}
	if d.top > len(choices)-room {
		d.top = len(choices) - room
	}
	if d.top < 0 {
		d.top = 0
	}
	shown := choices[min(d.top, len(choices)):]
	if len(shown) > room {
		shown = shown[:room]
	}
	for i, c := range shown {
		y := r.Y + 3 + i
		if y >= r.Y+r.H-1 {
			break
		}
		bg := color.Color(dirBg)
		fg := color.Color(dirFg)
		if d.top+i == d.selected {
			bg, fg = dirRowBg, dirAccent
			render.Fill(scr, uv.Rect(r.X, y, r.W, 1), bg)
		}
		render.Text(scr, r.X+2, y, clipPath(shortDir(c.dir), r.W-22), fg, bg)
		if c.worked && !c.last.IsZero() {
			when := when(c.last)
			if at := r.X + r.W - ansi.StringWidth(when) - 2; at > r.X+4 {
				render.Text(scr, at, y, when, dirDim, bg)
			}
		}
	}

	foot := "⏎ open a session there   esc close"
	if hidden := len(choices) - d.top - len(shown); hidden > 0 {
		foot = fmt.Sprintf("%d more below   %s", hidden, foot)
	}
	render.Text(scr, r.X+2, r.Y+r.H-1, clipBar(foot, r.W-4), dirDim, dirBg)
}

// clipPath shortens a path from the left, because the end of one is what tells
// you which directory it is. Clipping from the right leaves every path under a
// long parent looking identical.
func clipPath(p string, w int) string {
	if w <= 1 || ansi.StringWidth(p) <= w {
		return p
	}
	runes := []rune(p)
	keep := w - 1
	if keep > len(runes) {
		keep = len(runes)
	}
	return "…" + string(runes[len(runes)-keep:])
}
