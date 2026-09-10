package app

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/transcript"
)

func TestShrinkTopGivesUpTheTitleRow(t *testing.T) {
	got := shrinkTop(layout.Rect{X: 4, Y: 2, W: 20, H: 6}, titleH)
	want := layout.Rect{X: 4, Y: 3, W: 20, H: 5}
	if got != want {
		t.Errorf("shrinkTop = %v, want %v", got, want)
	}
	// A module that wants no title keeps every row it was given.
	full := layout.Rect{X: 4, Y: 2, W: 20, H: 6}
	if got := shrinkTop(full, 0); got != full {
		t.Errorf("shrinkTop(_, 0) = %v, want %v", got, full)
	}
	// A pane too short for both keeps its origin and has no content, rather
	// than reporting a negative height that callers would have to guard.
	if got := shrinkTop(layout.Rect{X: 1, Y: 1, W: 8, H: 1}, titleH); got.H != 0 {
		t.Errorf("a one-row pane leaves %d content rows, want 0", got.H)
	}
}

func TestTruncateMarksWhatItCut(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
	}{
		{"claude", 10, "claude"},
		{"claude", 6, "claude"},
		{"claude", 5, "clau…"},
		{"claude", 1, "…"},
		{"claude", 0, ""},
	} {
		if got := truncate(c.in, c.w); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

// The title of a pane holding a conversation reads as one line, not as two
// columns: a dot between the name and the model, and between the figures.
//
// It was a gap, and a gap of two spaces in the middle of a title reads as a
// column of a table the eye then looks for.
func TestThePaneTitleSeparatesTheNameFromTheReading(t *testing.T) {
	isolateState(t)
	a := newTestApp(t)
	const id = "pane-uuid"
	a.modules[1] = &namedSession{id: id}
	a.rects[1] = layout.Rect{X: 0, Y: 0, W: 60, H: 6}
	a.usage = map[string]transcript.Metrics{
		id: {Model: "claude-opus-5", Context: 454_000},
	}
	a.names = map[string]string{id: "Machines composites"}

	scr := uv.NewScreenBuffer(60, 6)
	a.drawPaneTitles(scr)

	var row strings.Builder
	for x := 0; x < 60; x++ {
		if c := scr.CellAt(x, 0); c != nil {
			row.WriteString(c.String())
		}
	}
	got := strings.TrimRight(row.String(), " ")
	want := "Machines composites · opus-5 · 454k ctx"
	if !strings.Contains(got, want) {
		t.Errorf("the title reads %q, want it to contain %q", got, want)
	}
}

// namedSession is a module holding one conversation and nothing else.
type namedSession struct {
	stub
	id string
}

func (n *namedSession) SessionID() string { return n.id }

// A pane that wants a title, which is what a conversation is: the name it
// wears is the one Claude Code gave it.
func (n *namedSession) Title() (string, bool) { return "claude", true }
