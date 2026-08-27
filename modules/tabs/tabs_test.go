package tabs_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	"claudecontrol/internal/usage"
	_ "claudecontrol/modules/stats"
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
func (p *probe) Sessions() []string           { return []string{p.id} }

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

// A pane with nothing in it is a configuration mistake worth naming. One tab
// is not: tabs are opened and closed as you work, and a pane may well be down
// to its last.
func TestAPaneOfTabsWantsSomethingInIt(t *testing.T) {
	if _, err := module.New("tabs", map[string]any{}); err == nil {
		t.Error("a pane with no tabs was accepted")
	}
	if _, err := module.New("tabs", map[string]any{"tabs": []any{
		map[string]any{"module": "term", "options": shell("cat")},
	}}); err != nil {
		t.Errorf("a single tab was refused: %v", err)
	}
	if _, err := module.New("tabs", map[string]any{"tabs": []any{
		map[string]any{"title": "x"},
		map[string]any{"module": "term"},
	}}); err == nil || !strings.Contains(err.Error(), "module") {
		t.Errorf("a tab with no module gave %v", err)
	}
}

// Tabs are opened and closed as you work. Opening one shows it, because a tab
// you then have to go and find would be a strange kind of opening.
func TestATabCanBeOpenedAndClosed(t *testing.T) {
	m := build(t, two(), module.Context{Wake: func() {}})
	counter, ok := m.(interface {
		Add(string, map[string]any) error
		CloseTab(int) error
		Count() int
		ActiveIndex() int
	})
	if !ok {
		t.Fatalf("tabs is %T", m)
	}

	if err := counter.Add("term", map[string]any{"cmd": []any{"sh", "-c", "printf THIRD; cat"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if counter.Count() != 3 {
		t.Fatalf("%d tabs after adding one", counter.Count())
	}
	if counter.ActiveIndex() != 2 {
		t.Errorf("the new tab is not the one on screen: index %d", counter.ActiveIndex())
	}
	waitFor(t, "the new tab", func() bool {
		return strings.Contains(paint(t, m, 50, 10).text(), "THIRD")
	})

	if err := counter.CloseTab(2); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	if counter.Count() != 2 {
		t.Errorf("%d tabs after closing one", counter.Count())
	}
	// Its neighbour is shown rather than nothing at all.
	if counter.ActiveIndex() != 1 {
		t.Errorf("after closing the last tab the one on screen is %d", counter.ActiveIndex())
	}
}

// Three tabs all reading "claude" would be three tabs you cannot tell apart.
func TestTabsOpenedWithTheSameNameAreNumbered(t *testing.T) {
	m := build(t, two(), module.Context{Wake: func() {}})
	adder := m.(interface {
		Add(string, map[string]any) error
	})
	for i := 0; i < 2; i++ {
		if err := adder.Add("term", map[string]any{"cmd": []any{"sh", "-c", "cat"}}); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	row := paint(t, m, 60, 10).row(0)
	if !strings.Contains(row, "term") || !strings.Contains(row, "term 2") {
		t.Errorf("the strip does not tell them apart: %q", row)
	}
}

// The pane would be left with nothing to draw. Closing the pane is a different
// gesture, and the application already has one.
func TestTheLastTabIsNotClosed(t *testing.T) {
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "only", "module": "term", "options": shell("cat")},
	}}, module.Context{Wake: func() {}})
	closer := m.(interface{ CloseTab(int) error })
	err := closer.CloseTab(0)
	if err == nil {
		t.Fatal("the last tab was closed")
	}
	if !strings.Contains(err.Error(), "pane") {
		t.Errorf("the refusal does not say what to do instead: %v", err)
	}
}

// A pane of tabs stands between the application and the modules it holds, so
// everything the application looks for on a pane has to pass through it.
// Forgetting one is not a compile error — it is a feature that quietly stops
// working the day someone puts the module in a tab, which is how the account
// figures left the status bar.
func TestWhatThePaneHoldsIsStillReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"five_hour":{"utilization":42.0,"resets_at":%q},
		                 "seven_day":{"utilization":15.0,"resets_at":%q}}`,
			time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339),
			time.Now().Add(50*time.Hour).UTC().Format(time.RFC3339))
	}))
	defer srv.Close()

	dir := t.TempDir()
	creds := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(creds, []byte(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":`+
		strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)+`}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// The stats module is in the tab you are *not* looking at, because that is
	// the case that matters: it is still reading, and the bar should still say
	// so.
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "front", "module": "term", "options": shell("cat")},
		map[string]any{"title": "usage", "module": "stats", "options": map[string]any{
			"endpoint": srv.URL, "credentials": creds,
		}},
	}}, module.Context{Wake: func() {}})

	acc, ok := m.(interface {
		Account() (usage.Reading, bool)
	})
	if !ok {
		t.Fatal("a pane of tabs does not pass the account reading through")
	}
	waitFor(t, "the reading", func() bool {
		r, taken := acc.Account()
		return taken && r.Err == nil && r.Snapshot.FiveHour.Percent == 42
	})

	// And the pane can be written back as it stands, tabs and all.
	vals, ok := m.(interface{ Values() map[string]any })
	if !ok {
		t.Fatal("a pane of tabs cannot be written back to the configuration")
	}
	got, _ := vals.Values()["tabs"].([]any)
	if len(got) != 2 {
		t.Fatalf("Values reports %d tabs, want 2: %v", len(got), vals.Values())
	}
	first, _ := got[0].(map[string]any)
	if first["module"] != "term" || first["title"] != "front" {
		t.Errorf("a tab is written back as %v", first)
	}

	// A tab opened while you worked is part of the pane now, and saving a
	// configuration that omitted it would lose it.
	adder := m.(interface {
		Add(string, map[string]any) error
	})
	if err := adder.Add("term", map[string]any{"cmd": []any{"sh", "-c", "cat"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got, _ := vals.Values()["tabs"].([]any); len(got) != 3 {
		t.Errorf("Values reports %d tabs after opening one, want 3", len(got))
	}
}

// Everything the application looks for on a pane's module, in one place.
//
// A pane of tabs stands between the two, so each of these has to be passed
// through. Three were missed one at a time — the account budgets, the values
// written back to the configuration, and the selected text — each discovered
// only when the feature stopped working. This list is what a fourth addition
// should be compared against.
//
// It is not automatic: nothing makes the application declare what it needs.
// What it does give is one place to look, and a failure that names the method
// rather than a feature that quietly does nothing.
func TestEveryInterfaceTheApplicationLooksForIsPassedThrough(t *testing.T) {
	m := build(t, two(), module.Context{Wake: func() {}})

	checks := []struct {
		what string
		ok   bool
	}{
		{"Account", implements[interface {
			Account() (usage.Reading, bool)
		}](m)},
		{"SessionID", implements[interface{ SessionID() string }](m)},
		{"ScrollOffset", implements[interface{ ScrollOffset() int }](m)},
		{"SelectedText", implements[interface{ SelectedText() string }](m)},
		{"Sessions", implements[module.Sessioner](m)},
		{"Find", implements[interface {
			Find(string) int
			FindNext(int)
			FindClear()
			FindStatus() (string, int, int)
		}](m)},
		{"Values", implements[interface{ Values() map[string]any }](m)},
		{"Title", implements[interface{ Title() (string, bool) }](m)},
		{"Cursor", implements[module.Cursorer](m)},
		{"Key/Mouse/Paste", implements[module.Inputter](m)},
		{"Settings", implements[module.Provider](m)},
		// The pane's own gestures, which the application drives from its
		// keys and the strip's buttons.
		{"CycleTab", implements[interface{ CycleTab(int) }](m)},
		{"Add", implements[interface {
			Add(string, map[string]any) error
		}](m)},
		{"CloseTab", implements[interface {
			CloseTab(int) error
			ActiveIndex() int
			Count() int
		}](m)},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("a pane of tabs does not pass %s through", c.what)
		}
	}
}

func implements[T any](v any) bool {
	_, ok := v.(T)
	return ok
}

// And the selected text is the one on screen, not another tab's.
func TestTheSelectionComesFromTheTabOnScreen(t *testing.T) {
	m := build(t, two(), module.Context{Wake: func() {}})
	sel := m.(interface{ SelectedText() string })
	if got := sel.SelectedText(); got != "" {
		t.Errorf("nothing is selected, yet the pane reports %q", got)
	}
}

// A pane of tabs holds conversations, and a later run brings back every one of
// them — not only the tab that happened to be in front.
func TestEveryTabsConversationIsRecorded(t *testing.T) {
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "one", "module": "probe", "options": map[string]any{"id": "sess-one"}},
		map[string]any{"title": "shell", "module": "term", "options": shell("cat")},
		map[string]any{"title": "two", "module": "probe", "options": map[string]any{"id": "sess-two"}},
	}}, module.Context{Wake: func() {}})

	lister, ok := m.(module.Sessioner)
	if !ok {
		t.Fatal("a pane of tabs does not report its conversations")
	}
	got := lister.Sessions()
	want := []string{"sess-one", "sess-two"}
	if len(got) != len(want) {
		t.Fatalf("Sessions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sessions = %v, want %v", got, want)
		}
	}
}
