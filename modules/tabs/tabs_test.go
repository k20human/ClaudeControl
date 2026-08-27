package tabs_test

import (
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/indicator"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
	_ "claudecontrol/modules/tabs"
	_ "claudecontrol/modules/term"
)

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
	if x < 0 || y < 0 || x >= g.w || y >= g.h || c == nil {
		return
	}
	g.cells[y*g.w+x] = *c
}
func (g *grid) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }
func (g *grid) row(y int) string {
	var b strings.Builder
	for x := 0; x < g.w; x++ {
		b.WriteString(g.cells[y*g.w+x].Content)
	}
	return strings.TrimRight(b.String(), " ")
}
func (g *grid) text() string {
	var b strings.Builder
	for y := 0; y < g.h; y++ {
		b.WriteString(g.row(y))
		b.WriteByte('\n')
	}
	return b.String()
}

type tabber interface {
	module.Module
	Select(int)
	CycleTab(int)
	ActiveTitle() string
}

func shell(script string) map[string]any {
	return map[string]any{"cmd": []any{"sh", "-c", script}}
}

func build(t *testing.T, cfg map[string]any, ctx module.Context) tabber {
	t.Helper()
	built, err := module.New("tabs", cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	m, ok := built.(tabber)
	if !ok {
		t.Fatalf("tabs is %T", built)
	}
	if err := m.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Resize(50, 10); err != nil {
		t.Fatalf("resize: %v", err)
	}
	return m
}

func paint(t *testing.T, m module.Module, w, h int) *grid {
	t.Helper()
	g := newGrid(w, h)
	m.Draw(g, uv.Rect(0, 0, w, h))
	return g
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func two() map[string]any {
	return map[string]any{"tabs": []any{
		map[string]any{"title": "one", "module": "term", "options": shell("printf FIRST; cat")},
		map[string]any{"title": "two", "module": "term", "options": shell("printf SECOND; cat")},
	}}
}

// Every module in the pane runs whether or not it is on screen: a supervisor
// still supervises from a hidden tab. Only drawing is skipped.
func TestEveryTabRunsAndOnlyOneIsDrawn(t *testing.T) {
	m := build(t, two(), module.Context{Wake: func() {}})

	var out string
	waitFor(t, "the first tab", func() bool {
		out = paint(t, m, 50, 10).text()
		return strings.Contains(out, "FIRST")
	})
	if strings.Contains(out, "SECOND") {
		t.Errorf("both tabs were drawn at once:\n%s", out)
	}

	m.Select(1)
	waitFor(t, "the second tab", func() bool {
		out = paint(t, m, 50, 10).text()
		return strings.Contains(out, "SECOND")
	})
	// It had been running all along, which is why its output is already there.
	if strings.Contains(out, "FIRST") {
		t.Errorf("the first tab is still drawn:\n%s", out)
	}
}

// The strip replaces the pane title rather than adding a row: the point of
// tabs is that space is short.
func TestTheStripTakesTheTitleRowRatherThanAnother(t *testing.T) {
	m := build(t, two(), module.Context{Wake: func() {}})
	titler, ok := m.(interface{ Title() (string, bool) })
	if !ok {
		t.Fatal("tabs does not publish a title")
	}
	if _, want := titler.Title(); want {
		t.Error("tabs asked for a pane title as well as its strip")
	}

	g := paint(t, m, 50, 10)
	if !strings.Contains(g.row(0), "one") || !strings.Contains(g.row(0), "two") {
		t.Errorf("the strip is not on the first row: %q", g.row(0))
	}
}

// Cycling wraps in both directions, because a pane of tabs is a ring.
func TestCyclingWrapsBothWays(t *testing.T) {
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "a", "module": "term", "options": shell("cat")},
		map[string]any{"title": "b", "module": "term", "options": shell("cat")},
		map[string]any{"title": "c", "module": "term", "options": shell("cat")},
	}}, module.Context{Wake: func() {}})

	for _, want := range []string{"b", "c", "a"} {
		m.CycleTab(1)
		if got := m.ActiveTitle(); got != want {
			t.Fatalf("forward reached %q, want %q", got, want)
		}
	}
	for _, want := range []string{"c", "b", "a"} {
		m.CycleTab(-1)
		if got := m.ActiveTitle(); got != want {
			t.Fatalf("backward reached %q, want %q", got, want)
		}
	}
}

// probe is a module that holds a session id, which is what a tab needs before
// it can carry a mark. Only the Claude module does in the product; a stand-in
// keeps this test off the real binary.
type probe struct{ id string }

func (p *probe) Init(module.Context) error    { return nil }
func (p *probe) Resize(int, int) error        { return nil }
func (p *probe) Draw(uv.Screen, uv.Rectangle) {}
func (p *probe) Close() error                 { return nil }
func (p *probe) SessionID() string            { return p.id }

func init() {
	module.Register("probe", func(cfg map[string]any) (module.Module, error) {
		id, _ := cfg["id"].(string)
		return &probe{id: id}, nil
	})
}

// A session doing something from a tab you are not looking at has to be able
// to say so: a hidden tab is otherwise a session you have forgotten. The mark
// is the same one the pane titles and the terminal's own title use.
func TestAHiddenTabCarriesItsSessionsMark(t *testing.T) {
	b := bus.New()
	defer b.Close()
	p := pool.New(b)

	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "plain", "module": "term", "options": shell("cat")},
		map[string]any{"title": "hidden", "module": "probe", "options": map[string]any{"id": "sess-hidden"}},
	}}, module.Context{Bus: b, Wake: func() {}})

	s, err := session.Start(session.Spec{
		ID: "sess-hidden", Argv: []string{"sh", "-c", "sleep 30"}, Width: 20, Height: 4,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close()
	p.Add(s, "hidden", "")

	for _, state := range []pool.State{pool.StateWaiting, pool.StateExited} {
		p.SetState(s.ID, state)
		want := indicator.Glyph(state, time.Now())
		waitFor(t, "the mark for "+state.String(), func() bool {
			return strings.Contains(paint(t, m, 50, 10).row(0), want)
		})
	}

	// The tab with no session gets no mark: a shape that means a state must
	// never appear where there is no state. The mark is drawn before the
	// title, so a bare label at the start of the strip is the whole proof.
	if row := paint(t, m, 50, 10).row(0); !strings.HasPrefix(row, " plain ") {
		t.Errorf("the session-less tab carries a mark: %q", row)
	}
}

// A label that does not fit is dropped whole and counted, so the strip never
// lies about how many tabs there are.
func TestALabelThatDoesNotFitIsDroppedAndCounted(t *testing.T) {
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "supervisor", "module": "term", "options": shell("cat")},
		map[string]any{"title": "statistics", "module": "term", "options": shell("cat")},
		map[string]any{"title": "hologram", "module": "term", "options": shell("cat")},
	}}, module.Context{Wake: func() {}})

	row := paint(t, m, 18, 6).row(0)
	if strings.Contains(row, "…") {
		t.Errorf("a label was cut: %q", row)
	}
	if !strings.Contains(row, "+") {
		t.Errorf("the strip does not say what it dropped: %q", row)
	}
}

// One tab is a pane with a wasted row and none is a pane with nothing in it.
func TestAPaneOfTabsWantsAtLeastTwo(t *testing.T) {
	for _, cfg := range []map[string]any{
		{},
		{"tabs": []any{map[string]any{"module": "term"}}},
	} {
		if _, err := module.New("tabs", cfg); err == nil {
			t.Errorf("accepted %v", cfg)
		}
	}
	if _, err := module.New("tabs", map[string]any{"tabs": []any{
		map[string]any{"title": "x"},
		map[string]any{"module": "term"},
	}}); err == nil || !strings.Contains(err.Error(), "module") {
		t.Errorf("a tab with no module gave %v", err)
	}
}
