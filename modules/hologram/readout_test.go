package hologram

import (
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
	"claudecontrol/internal/transcript"
)

// fakeSession is an identity without a process: noteTransitions reads nothing
// but the id.
func fakeSession(id string) *session.Session { return &session.Session{ID: session.ID(id)} }

// The ambient line is decoration, and decoration on a control panel must never
// be mistakable for a statement about what the machine is doing. Every line
// has to come from the pool of the state the sessions are actually in.
func TestTheAmbientLineAlwaysMatchesTheRealState(t *testing.T) {
	r := newReadout(1)
	now := time.Now()
	for _, state := range []string{"idle", "working", "waiting", "exited"} {
		r.Observe(state, now)
		got := strings.TrimPrefix(r.Lines(now)[0], "▸ ")
		if !contains(flavours[state], got) {
			t.Errorf("in state %q the line reads %q, which belongs to another state",
				state, got)
		}
		now = now.Add(time.Second)
	}
}

// No line may cite a figure or name an operation: a phrase claiming work that
// is not happening costs you the right to believe the rest of the panel.
func TestNoAmbientLineClaimsAnOperation(t *testing.T) {
	for state, pool := range flavours {
		for _, line := range pool {
			if strings.ContainsAny(line, "0123456789%") {
				t.Errorf("%s: %q cites a figure", state, line)
			}
			for _, word := range []string{"analys", "scan", "comput", "process", "index", "query"} {
				if strings.Contains(line, word) {
					t.Errorf("%s: %q names an operation that nothing performs", state, line)
				}
			}
		}
	}
}

// It changes when the state changes, and holds otherwise — a phrase rewritten
// every second reads as noise.
func TestTheAmbientLineHoldsBetweenChanges(t *testing.T) {
	r := newReadout(3)
	start := time.Now()
	r.Observe("working", start)
	first := r.Lines(start)[0]

	r.Observe("working", start.Add(flavourHold/2))
	if got := r.Lines(start)[0]; got != first {
		t.Errorf("the line changed within its hold: %q then %q", first, got)
	}

	// Past the hold it may be redrawn; over several holds it must actually
	// vary, or the pool is not being used.
	varied := false
	for i := 1; i <= 40 && !varied; i++ {
		r.Observe("working", start.Add(time.Duration(i)*flavourHold))
		varied = r.Lines(start)[0] != first
	}
	if !varied {
		t.Error("the line never changed over forty holds")
	}
}

// The log reports what happened, newest first, and forgets the oldest rather
// than growing without bound.
func TestTheLogKeepsTheMostRecentEvents(t *testing.T) {
	r := newReadout(5)
	base := time.Now()
	for i := 0; i < logDepth+5; i++ {
		r.Note(base.Add(time.Duration(i)*time.Minute), "event"+string(rune('a'+i)))
	}
	lines := r.Lines(base)
	if got := len(lines) - 2; got != logDepth {
		t.Fatalf("the log holds %d entries, want %d", got, logDepth)
	}
	if !strings.Contains(lines[2], "event"+string(rune('a'+logDepth+4))) {
		t.Errorf("the newest event is not first: %q", lines[2])
	}
	if strings.Contains(strings.Join(lines, "\n"), "eventa") {
		t.Error("the oldest event was kept")
	}
}

// Only changes are logged: the pool announces its whole state on every event,
// and repeating "thinking" once a second would bury what actually happened.
func TestOnlyTransitionsReachTheLog(t *testing.T) {
	m := &Module{readout: newReadout(7), prev: map[string]poolState{}, side: "right"}
	entries := []*pool.Entry{{Session: fakeSession("s1"), Title: "one", State: pool.StateIdle}}

	m.noteTransitions(entries)
	first := len(m.readout.log)
	if first != 1 || !strings.Contains(m.readout.log[0].text, "started") {
		t.Fatalf("a new session logged %v", m.readout.log)
	}

	m.noteTransitions(entries)
	if len(m.readout.log) != first {
		t.Errorf("an unchanged announcement logged %v", m.readout.log)
	}

	entries[0].State = pool.StateWorking
	m.noteTransitions(entries)
	if got := m.readout.log[len(m.readout.log)-1].text; !strings.Contains(got, "thinking") {
		t.Errorf("the transition to working logged %q", got)
	}

	// With one session the name would be on every line and say nothing.
	if strings.Contains(m.readout.log[0].text, "one") {
		t.Errorf("a single session was named: %q", m.readout.log[0].text)
	}
	entries = append(entries, &pool.Entry{Session: fakeSession("s2"), Title: "two", State: pool.StateIdle})
	m.noteTransitions(entries)
	if got := m.readout.log[len(m.readout.log)-1].text; !strings.Contains(got, "two") {
		t.Errorf("with two sessions the event is not named: %q", got)
	}
}

// A turn is described, never quoted: a decorative panel that echoes what
// Claude writes puts your work on any screen the terminal is shown on.
func TestATurnIsDescribedNotQuoted(t *testing.T) {
	got := turnLine(transcript.Metrics{Context: 34_000, Model: "claude-opus-5"})
	if !strings.Contains(got, "34k") {
		t.Errorf("turnLine = %q, want the context in it", got)
	}
	if got := turnLine(transcript.Metrics{}); got != "turn" {
		t.Errorf("a turn with no context reads %q", got)
	}
}

// The column and the sphere have to agree on the width, or the sphere is built
// for one and painted into the other.
func TestTheColumnAppearsOnlyWhereThereIsRoom(t *testing.T) {
	for _, c := range []struct {
		w    int
		side string
		want int
	}{
		{80, "right", readoutW},
		{80, "left", readoutW},
		{80, "off", 0},
		{readoutMinPane - 1, "right", 0},
		{46, "right", 23},
	} {
		if got := columnW(c.w, c.side); got != c.want {
			t.Errorf("columnW(%d, %q) = %d, want %d", c.w, c.side, got, c.want)
		}
	}

	area := uv.Rect(10, 2, 80, 20)
	sphere, column, ok := readoutSplit(area, "right")
	if !ok {
		t.Fatal("no column at eighty columns")
	}
	if sphere.Dx()+column.Dx() != area.Dx() {
		t.Errorf("the split loses columns: %d + %d against %d", sphere.Dx(), column.Dx(), area.Dx())
	}
	if column.Min.X != area.Max.X-readoutW {
		t.Errorf("the right-hand column starts at %d, want %d", column.Min.X, area.Max.X-readoutW)
	}
	left, lcol, _ := readoutSplit(area, "left")
	if lcol.Min.X != area.Min.X || left.Min.X != area.Min.X+readoutW {
		t.Errorf("the left-hand split is wrong: column at %d, sphere at %d", lcol.Min.X, left.Min.X)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// The corner line asserts nothing — no activity, no figure, no state — which
// is what makes it safe to show whatever is happening. A disposition cannot be
// false the way a claim can.
func TestTheCornerLineClaimsNothing(t *testing.T) {
	for _, line := range maxims {
		if strings.ContainsAny(line, "0123456789%") {
			t.Errorf("%q cites a figure", line)
		}
		for _, word := range []string{"running", "thinking", "waiting", "analys", "scan", "process"} {
			if strings.Contains(line, word) {
				t.Errorf("%q says what the machine is doing", line)
			}
		}
	}
}

// It has its own clock, and does not follow the state: two lines changing
// together would read as one animation rather than two thoughts.
func TestTheCornerLineIgnoresTheState(t *testing.T) {
	r := newReadout(11)
	now := time.Now()
	first := r.Maxim()

	for i, state := range []string{"working", "waiting", "exited", "idle"} {
		r.Observe(state, now.Add(time.Duration(i)*time.Second))
		if r.Maxim() != first {
			t.Fatalf("the corner line changed with the state, at %q", state)
		}
	}

	varied := false
	for i := 1; i <= 40 && !varied; i++ {
		r.Observe("idle", now.Add(time.Duration(i)*maximHold))
		varied = r.Maxim() != first
	}
	if !varied {
		t.Error("the corner line never changed over forty holds")
	}
}

// A pane too narrow drops it whole: an aphorism cut in half is not a shorter
// aphorism.
func TestTheCornerLineIsDroppedRatherThanCut(t *testing.T) {
	r := newReadout(13)
	wide := newGrid(60, 10)
	r.drawMaxim(wide, uv.Rect(0, 0, 60, 10))
	if !strings.Contains(wide.text(), r.Maxim()) {
		t.Errorf("the corner line is missing from a wide pane:\n%s", wide.text())
	}
	// The bottom row, right-aligned.
	if got := strings.TrimRight(wide.row(9), " "); !strings.HasSuffix(got, r.Maxim()) {
		t.Errorf("the corner line is not in the corner: %q", got)
	}

	narrow := newGrid(12, 10)
	r.drawMaxim(narrow, uv.Rect(0, 0, 12, 10))
	if strings.Contains(narrow.text(), "…") {
		t.Errorf("a narrow pane cut it:\n%s", narrow.text())
	}
	if strings.TrimSpace(narrow.text()) != "" {
		t.Errorf("a narrow pane drew something:\n%s", narrow.text())
	}
}

// grid is a screen that remembers what was painted.
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
	return b.String()
}

func (g *grid) text() string {
	var b strings.Builder
	for y := 0; y < g.h; y++ {
		b.WriteString(strings.TrimRight(g.row(y), " "))
		b.WriteByte('\n')
	}
	return b.String()
}
