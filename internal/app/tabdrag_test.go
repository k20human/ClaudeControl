package app_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
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
		return strings.Contains(stripHalf(g, true), "goer")
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
		return strings.Contains(stripHalf(g, true), "goer")
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

// stripHalf is one half of the tab strip, safe on a screen that has not been
// drawn yet: a trimmed row is shorter than the screen is wide.
func stripHalf(g *screen, right bool) string {
	row := g.row(0)
	mid := g.w / 2
	if len(row) < mid {
		if right {
			return ""
		}
		return row
	}
	if right {
		return row[mid:]
	}
	return row[:mid]
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
	if !strings.Contains(stripHalf(g, false), "goer") {
		t.Errorf("the tab left its pane after the move was cancelled:\n%s", dump(g))
	}
	waitForRow(t, snap, H-1, "2 panes")
}

// The arrangement you leave is the one you get back. This is the gesture and
// the saving together, which is the only way either is worth anything: move a
// tab, quit with the box ticked, and open again on the same configuration.
func TestTheArrangementSurvivesQuitting(t *testing.T) {
	const W, H = 120, 20
	cfg := twoTabbedPanes(t)
	state := t.TempDir()

	first, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, []string{"XDG_STATE_HOME=" + state})
	waitForAnywhere(t, snap, "LEFT-HERE")
	click(t, first, 10, 5)

	fromX, fromY := at(t, snap, "goer")
	dragTab(t, first, fromX, fromY, 3*W/4, H/2)
	waitFor(t, snap, func(g *screen) bool {
		return strings.Contains(stripHalf(g, true), "goer")
	}, "the tab to move")

	// Quit with the box as it comes: ticked.
	first.SendText("\x1bq")
	waitForAnywhere(t, snap, "save this layout")
	if g := snap(); !anywhere(g, "[x] save this layout") {
		t.Fatalf("the box is not ticked to begin with:\n%s", dump(g))
	}
	first.SendText("\r")
	time.Sleep(700 * time.Millisecond)

	// Open again on the same configuration, which has not been touched since.
	second, snap2 := runWithEnv(t, cfg, W, H, vt.Callbacks{}, []string{"XDG_STATE_HOME=" + state})
	_ = second
	waitFor(t, snap2, func(g *screen) bool {
		return strings.Contains(stripHalf(g, true), "goer")
	}, "the moved tab to come back on the right")
}

// And editing the configuration puts it back in charge, so a pane added there
// is a pane you see.
func TestEditingTheConfigurationBeatsTheSavedArrangement(t *testing.T) {
	const W, H = 120, 20
	cfg := twoTabbedPanes(t)
	state := t.TempDir()

	first, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, []string{"XDG_STATE_HOME=" + state})
	waitForAnywhere(t, snap, "LEFT-HERE")
	click(t, first, 10, 5)
	fromX, fromY := at(t, snap, "goer")
	dragTab(t, first, fromX, fromY, 3*W/4, H/2)
	waitFor(t, snap, func(g *screen) bool {
		return strings.Contains(stripHalf(g, true), "goer")
	}, "the tab to move")
	first.SendText("\x1bq")
	waitForAnywhere(t, snap, "save this layout")
	first.SendText("\r")
	time.Sleep(700 * time.Millisecond)

	// Touched since.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfg, later, later); err != nil {
		t.Fatal(err)
	}

	second, snap2 := runWithEnv(t, cfg, W, H, vt.Callbacks{}, []string{"XDG_STATE_HOME=" + state})
	_ = second
	waitFor(t, snap2, func(g *screen) bool {
		return strings.Contains(stripHalf(g, false), "goer")
	}, "the configuration's arrangement")
	waitForAnywhere(t, snap2, "config.yaml is newer")
}

// A tab is chrome, not guest content. The first click on an unfocused pane is
// swallowed so that moving between panes can never trigger something inside
// the one you land on — but that protects the guest, and a tab strip is not
// the guest. Having to click a pane before you can pick up one of its tabs is
// a rule nobody can see.
func TestATabCanBeDraggedFromAPaneThatIsNotFocused(t *testing.T) {
	const W, H = 120, 20
	// The right pane keeps a second tab, so taking one from it does not
	// remove the pane: this test is about reaching an unfocused strip, and a
	// collapsing layout would move the halves under the assertion.
	cfg := filepath.Join(t.TempDir(), "two.yaml")
	body := `layout:
  split: horizontal
  ratios: [1, 1]
  children:
    - module: tabs
      options:
        tabs:
          - { title: leftish, module: term, options: { cmd: [sh, -c, "printf LEFT-HERE; cat"] } }
    - module: tabs
      options:
        tabs:
          - { title: righty, module: term, options: { cmd: [sh, -c, "printf RIGHT-HERE; cat"] } }
          - { title: stayer, module: term, options: { cmd: [sh, -c, "printf STAYS-HERE; cat"] } }
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, snap := run(t, cfg, W, H)
	waitForAnywhere(t, snap, "LEFT-HERE")
	waitForAnywhere(t, snap, "righty")

	// Focus stays on the left pane; the tab comes from the right one.
	click(t, s, 10, 5)

	fromX, fromY := at(t, snap, "righty")
	dragTab(t, s, fromX, fromY, W/4, H/2)

	waitFor(t, snap, func(g *screen) bool {
		return strings.Contains(stripHalf(g, false), "righty")
	}, "the tab to arrive on the left")
	waitForAnywhere(t, snap, "RIGHT-HERE")
	// And the pane it came from is still there, with what it kept.
	waitFor(t, snap, func(g *screen) bool {
		return strings.Contains(stripHalf(g, true), "stayer")
	}, "the pane it came from to keep its other tab")
}

// And a plain click on an unfocused pane's tab selects it, rather than being
// spent on focusing the pane.
func TestClickingATabOfAnUnfocusedPaneSelectsIt(t *testing.T) {
	const W, H = 120, 20
	s, snap := run(t, twoTabbedPanes(t), W, H)
	waitForAnywhere(t, snap, "LEFT-HERE")
	click(t, s, 3*W/4, 5) // focus the right pane
	waitForAnywhere(t, snap, "RIGHT-HERE")

	// From the right pane, click the left pane's second tab directly.
	x, y := at(t, snap, "goer")
	click(t, s, x, y)
	waitForAnywhere(t, snap, "GOER-HERE")
}

// A pane holding a single tab, which is the shape a pane ends up in as soon as
// you have moved its others away — and the one a tab most needs to leave.
func oneAndThree(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "one.yaml")
	body := `layout:
  split: horizontal
  ratios: [2, 1]
  children:
    - module: tabs
      options:
        tabs:
          - { title: brain, module: term, options: { cmd: [sh, -c, "printf BRAIN-HERE; cat"] } }
          - { title: services, module: term, options: { cmd: [sh, -c, "printf SERVICES-HERE; cat"] } }
    - module: tabs
      options:
        tabs:
          - { title: lonely, module: term, options: { cmd: [sh, -c, "printf LONELY-HERE; cat"] } }
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The only tab of a pane can be dragged out of it, which takes the pane with
// it. A tab you cannot pick up is a tab you have to close and open again
// somewhere else, and for a conversation that means losing it.
func TestTheOnlyTabOfAPaneCanBeDragged(t *testing.T) {
	const W, H = 120, 20
	s, snap := run(t, oneAndThree(t), W, H)
	waitForAnywhere(t, snap, "BRAIN-HERE")
	waitForAnywhere(t, snap, "lonely")
	waitForRow(t, snap, H-1, "2 panes")

	// Straight at it, without focusing its pane first.
	fromX, fromY := at(t, snap, "lonely")
	dragTab(t, s, fromX, fromY, W/4, H/2)

	waitForRow(t, snap, H-1, "1 pane")
	waitForAnywhere(t, snap, "lonely")
	waitForAnywhere(t, snap, "LONELY-HERE")
}
