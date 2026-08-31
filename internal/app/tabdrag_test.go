package app_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// twoTabbedPanes is a split with a pane of tabs on each side: the arrangement
// a tab is moved between.
func twoTabbedPanes(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "tabs.yaml")
	body := `layout:
  split: horizontal
  ratios: [1, 1]
  children:
    - module: tabs
      options:
        tabs:
          - { title: leftish, module: term, options: { cmd: [sh, -c, "printf LEFT-HERE; cat"] } }
          - { title: goer, module: term, options: { cmd: [sh, -c, "printf GOER-HERE; cat"] } }
    - module: tabs
      options:
        tabs:
          - { title: righty, module: term, options: { cmd: [sh, -c, "printf RIGHT-HERE; cat"] } }
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// dragTab presses a tab's label, moves, and releases somewhere else.
func dragTab(t *testing.T, s sender, fromX, fromY, toX, toY int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<0;%d;%dM", fromX+1, fromY+1))
	time.Sleep(80 * time.Millisecond)
	// Two steps, because the first movement is what turns a press into a drag.
	midX, midY := (fromX+toX)/2, (fromY+toY)/2
	s.SendText(fmt.Sprintf("\x1b[<32;%d;%dM", midX+1, midY+1))
	time.Sleep(80 * time.Millisecond)
	s.SendText(fmt.Sprintf("\x1b[<32;%d;%dM", toX+1, toY+1))
	time.Sleep(80 * time.Millisecond)
	s.SendText(fmt.Sprintf("\x1b[<0;%d;%dm", toX+1, toY+1))
	time.Sleep(400 * time.Millisecond)
}

// at is where some text sits on screen.
func at(t *testing.T, snap func() *screen, want string) (x, y int) {
	t.Helper()
	g := snap()
	for row := 0; row < g.h; row++ {
		if c := columnOf(g.row(row), want); c >= 0 {
			return c, row
		}
	}
	t.Fatalf("%q is not on screen:\n%s", want, dump(g))
	return 0, 0
}

func dump(g *screen) string {
	var b strings.Builder
	for y := 0; y < g.h; y++ {
		b.WriteString(strings.TrimRight(g.row(y), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// Dropped on the middle of another pane of tabs, a tab joins that pane — with
// the process it was showing, which is the whole point.
func TestATabDroppedOnAnotherPaneJoinsIt(t *testing.T) {
	const W, H = 120, 20
	s, snap := run(t, twoTabbedPanes(t), W, H)
	waitForAnywhere(t, snap, "LEFT-HERE")
	waitForAnywhere(t, snap, "righty")

	// Focus the left pane first: the first click on an unfocused pane is
	// swallowed, here as everywhere.
	click(t, s, 10, 5)

	fromX, fromY := at(t, snap, "goer")
	toX, toY := 3*W/4, H/2
	dragTab(t, s, fromX, fromY, toX, toY)

	// The strip on the right now carries it, and the one on the left does not.
	waitFor(t, snap, func(g *screen) bool {
		right := g.row(0)[W/2:]
		return strings.Contains(right, "goer")
	}, "the tab to arrive on the right")

	// And its process came with it.
	waitForAnywhere(t, snap, "GOER-HERE")
}

// Dropped on an edge, a tab becomes a pane of its own.
func TestATabDroppedOnAnEdgeBecomesAPane(t *testing.T) {
	const W, H = 120, 20
	s, snap := run(t, twoTabbedPanes(t), W, H)
	waitForAnywhere(t, snap, "LEFT-HERE")
	click(t, s, 10, 5)

	fromX, fromY := at(t, snap, "goer")
	// The far right edge of the right-hand pane.
	dragTab(t, s, fromX, fromY, W-3, H/2)

	waitForAnywhere(t, snap, "GOER-HERE")
	waitFor(t, snap, func(g *screen) bool {
		return strings.Count(g.row(H-1), "panes") > 0 && strings.Contains(g.row(H-1), "3 panes")
	}, "a third pane")
}

// Dropped on the middle of a pane that is not tabs, the target becomes one,
// holding both.
func TestATabDroppedOnAPlainPaneWrapsIt(t *testing.T) {
	const W, H = 120, 20
	cfg := filepath.Join(t.TempDir(), "mixed.yaml")
	body := `layout:
  split: horizontal
  ratios: [1, 1]
  children:
    - module: tabs
      options:
        tabs:
          - { title: leftish, module: term, options: { cmd: [sh, -c, "printf LEFT-HERE; cat"] } }
          - { title: goer, module: term, options: { cmd: [sh, -c, "printf GOER-HERE; cat"] } }
    - module: term
      options: { cmd: [sh, -c, "printf PLAIN-HERE; cat"] }
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, snap := run(t, cfg, W, H)
	waitForAnywhere(t, snap, "PLAIN-HERE")
	click(t, s, 10, 5)

	fromX, fromY := at(t, snap, "goer")
	dragTab(t, s, fromX, fromY, 3*W/4, H/2)

	// The right-hand pane now has a strip carrying both.
	waitFor(t, snap, func(g *screen) bool {
		right := g.row(0)[W/2:]
		return strings.Contains(right, "goer")
	}, "a strip on the right")
	waitForAnywhere(t, snap, "GOER-HERE")

	// What was there is now a tab beside it. The arriving tab is the one on
	// screen, so the proof it survived is that going back to it brings its
	// output with it.
	tx, ty := at(t, snap, "term")
	click(t, s, tx, ty)
	waitForAnywhere(t, snap, "PLAIN-HERE")
}

// Dragging the last tab out of a pane takes the pane with it: what is left
// behind is nothing, and a pane drawing nothing is not worth keeping.
func TestDraggingTheLastTabAwayRemovesThePane(t *testing.T) {
	const W, H = 120, 20
	s, snap := run(t, twoTabbedPanes(t), W, H)
	waitForAnywhere(t, snap, "RIGHT-HERE")
	waitForRow(t, snap, H-1, "2 panes")

	// Focus the right pane, whose only tab is righty.
	click(t, s, 3*W/4, 5)
	fromX, fromY := at(t, snap, "righty")
	dragTab(t, s, fromX, fromY, W/4, H/2)

	waitForRow(t, snap, H-1, "1 pane")
	waitForAnywhere(t, snap, "RIGHT-HERE")
}

// waitFor polls a condition against the screen.
func waitFor(t *testing.T, snap func() *screen, cond func(*screen) bool, what string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cond(snap()) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s:\n%s", what, dump(snap()))
}

// Dragging a tab along its own strip reorders it, and shows no preview: the
// strip is already saying where the tab is.
func TestATabDraggedAlongItsOwnStripIsReordered(t *testing.T) {
	const W, H = 120, 20
	s, snap := run(t, twoTabbedPanes(t), W, H)
	waitForAnywhere(t, snap, "LEFT-HERE")
	click(t, s, 10, 5)

	strip := snap().row(0)
	first, second := columnOf(strip, "leftish"), columnOf(strip, "goer")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("the strip starts as %q", strip)
	}

	dragTab(t, s, second, 0, first, 0)

	waitFor(t, snap, func(g *screen) bool {
		row := g.row(0)
		a, b := columnOf(row, "goer"), columnOf(row, "leftish")
		return a >= 0 && b >= 0 && a < b
	}, "goer to move ahead of leftish")

	// And the tab you dragged is the one you are still looking at.
	waitForAnywhere(t, snap, "GOER-HERE")
}

// A move given up on costs nothing: escape puts the tab back.
func TestEscapeCancelsATabDrag(t *testing.T) {
	const W, H = 120, 20
	s, snap := run(t, twoTabbedPanes(t), W, H)
	waitForAnywhere(t, snap, "LEFT-HERE")
	click(t, s, 10, 5)

	fromX, fromY := at(t, snap, "goer")
	s.SendText(fmt.Sprintf("\x1b[<0;%d;%dM", fromX+1, fromY+1))
	time.Sleep(80 * time.Millisecond)
	s.SendText(fmt.Sprintf("\x1b[<32;%d;%dM", 3*W/4, H/2))
	time.Sleep(120 * time.Millisecond)
	s.SendText("\x1b") // escape
	time.Sleep(300 * time.Millisecond)
	s.SendText(fmt.Sprintf("\x1b[<0;%d;%dm", 3*W/4+1, H/2+1))
	time.Sleep(300 * time.Millisecond)

	// Still on the left, still two panes.
	g := snap()
	if !strings.Contains(g.row(0)[:W/2], "goer") {
		t.Errorf("the tab left its pane after the move was cancelled:\n%s", dump(g))
	}
	waitForRow(t, snap, H-1, "2 panes")
}
