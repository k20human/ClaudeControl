package app

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/layout"
)

// grid is a uv.Screen the internal tests can draw into and then read back.
type grid struct {
	w, h  int
	cells []uv.Cell
}

func newGrid(w, h int) *grid {
	g := &grid{w: w, h: h, cells: make([]uv.Cell, w*h)}
	for i := range g.cells {
		g.cells[i] = uv.EmptyCell
	}
	return g
}

func (g *grid) Bounds() uv.Rectangle { return uv.Rect(0, 0, g.w, g.h) }

func (g *grid) CellAt(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return nil
	}
	return &g.cells[y*g.w+x]
}

func (g *grid) SetCell(x, y int, c *uv.Cell) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return
	}
	if c == nil {
		g.cells[y*g.w+x] = uv.EmptyCell
		return
	}
	g.cells[y*g.w+x] = *c
}

func (g *grid) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }

func (g *grid) String() string {
	var sb strings.Builder
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			sb.WriteString(g.cells[y*g.w+x].String())
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (g *grid) contains(s string) bool { return strings.Contains(g.String(), s) }

func paletteApp(t *testing.T) *App {
	t.Helper()
	return settingsApp(t, "")
}

// The palette is built from the binding table, like the help panel: an action
// added without a palette entry is impossible.
func TestThePaletteListsEveryNamedBinding(t *testing.T) {
	a := paletteApp(t)
	a.togglePalette()
	if a.palette == nil {
		t.Fatal("toggling did not open the palette")
	}

	got := map[string]bool{}
	for _, e := range a.paletteEntries() {
		got[e.label] = true
	}
	for _, b := range bindings {
		if b.desc == "" || b.run == nil {
			continue
		}
		if !got[b.display] {
			t.Errorf("the palette is missing %q", b.display)
		}
	}
}

// Matching is on both the keystroke and its description, because you remember
// one or the other, not reliably both.
func TestMatchingLooksAtTheDescriptionToo(t *testing.T) {
	cases := []struct {
		query, label, desc string
		want               bool
	}{
		{"", "alt+n", "new session", true},
		{"new", "alt+n", "new session", true},
		{"alt+n", "alt+n", "new session", true},
		{"NEW", "alt+n", "new session", true},
		{"sess", "alt+n", "new session", true},
		{"zoom", "alt+n", "new session", false},
	}
	for _, c := range cases {
		if got := matches(c.query, c.label, c.desc); got != c.want {
			t.Errorf("matches(%q, %q, %q) = %v, want %v", c.query, c.label, c.desc, got, c.want)
		}
	}
}

// Typing narrows the list; backspace widens it again.
func TestTypingFiltersAndBackspaceRestores(t *testing.T) {
	a := paletteApp(t)
	a.togglePalette()
	all := len(a.paletteVisible())

	a.paletteType("zoom")
	narrowed := len(a.paletteVisible())
	if narrowed == 0 {
		t.Fatal("filtering on zoom matched nothing")
	}
	if narrowed >= all {
		t.Fatalf("filtering on zoom left %d of %d entries", narrowed, all)
	}

	for i := 0; i < 4; i++ {
		a.paletteBackspace()
	}
	if got := len(a.paletteVisible()); got != all {
		t.Fatalf("%d entries after clearing the query, want %d", got, all)
	}
}

// Choosing runs the action and closes the palette.
func TestChoosingRunsTheActionAndCloses(t *testing.T) {
	a := paletteApp(t)
	a.togglePalette()
	a.paletteType("zoom")

	before := a.zoomed
	a.paletteChoose()
	if a.palette != nil {
		t.Error("the palette stayed open after a choice")
	}
	if a.zoomed == before {
		t.Error("choosing zoom did nothing")
	}
}

// A query that matches nothing must not run whatever happened to be first.
func TestChoosingNothingRunsNothing(t *testing.T) {
	a := paletteApp(t)
	a.togglePalette()
	a.paletteType("no-such-action-anywhere")
	if got := len(a.paletteVisible()); got != 0 {
		t.Fatalf("%d entries matched a nonsense query", got)
	}
	before := a.zoomed
	a.paletteChoose()
	if a.zoomed != before {
		t.Error("choosing from an empty list ran something")
	}
	if a.palette == nil {
		t.Error("the palette closed on a choice it could not make")
	}
}

func TestMatchIsCaseInsensitiveOnBothSides(t *testing.T) {
	if !matches("ZOOM", "alt+z", "zoom / restore") {
		t.Error("an upper-case query did not match a lower-case description")
	}
}

// Every action the help panel names must be reachable from the palette too.
// A display-only row — one that describes a keystroke without an action behind
// it — would be listed in help and missing here.
func TestHelpAndThePaletteAgree(t *testing.T) {
	a := paletteApp(t)
	a.togglePalette()

	inPalette := map[string]bool{}
	for _, e := range a.paletteEntries() {
		inPalette[e.label] = true
	}
	for _, line := range helpLines() {
		if line[0] == "" || line[0] == "click" || line[0] == "click again" ||
			line[0] == "drag a divider" {
			continue // gestures, not keystrokes
		}
		if !inPalette[line[0]] {
			t.Errorf("help lists %q but the palette cannot reach it", line[0])
		}
	}
}

// A palette that shows only part of its list must say so, or the entry you
// wanted looks absent rather than below the fold.
func TestThePaletteSaysWhatItCouldNotShow(t *testing.T) {
	a := paletteApp(t)
	a.area = layout.Rect{X: 0, Y: 0, W: 80, H: 12}
	a.togglePalette()

	r := a.palettePanelRect()
	rows := r.H - 5
	if len(a.paletteVisible()) <= rows {
		t.Skipf("the panel fits all %d entries; nothing to hide", len(a.paletteVisible()))
	}

	g := newGrid(80, 12)
	a.drawPalette(g)
	if !g.contains("more — keep typing") {
		t.Fatalf("the palette hid entries without saying so:\n%s", g)
	}
}
