package app

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
)

func dragApp(t *testing.T, w, h int) *App {
	t.Helper()
	isolateState(t)
	a := &App{
		root: &layout.Node{
			Kind:        layout.KindSplit,
			Orientation: layout.Horizontal,
			Ratios:      []int{1, 1},
			Children: []*layout.Node{
				{Kind: layout.KindLeaf, PaneID: 1},
				{Kind: layout.KindLeaf, PaneID: 2},
			},
		},
		modules:  map[layout.PaneID]module.Module{1: &stub{}, 2: &stub{}},
		bus:      testBus,
		pool:     pool.New(testBus),
		area:     layout.Rect{X: 0, Y: 0, W: w, H: h},
		focus:    1,
		prev:     1,
		nextPane: 2,
		hoverDiv: -1,
		wake:     make(chan struct{}, 1),
	}
	a.relayout()
	return a
}

// One cell of pointer movement must move the divider by one cell. Expressing
// the drag in ratio units instead loses all precision: with ratios [1 1] over
// 59 columns, a thirty-cell drag rounded down to a single unit and anything
// shorter did nothing at all.
func TestDragMovesTheDividerCellForCell(t *testing.T) {
	a := dragApp(t, 60, 12)
	before := a.rects[1].W
	divX := a.divs[0].Rect.X

	a.handleMouse(uv.MouseClickEvent{X: divX, Y: 5}, uv.Mouse{X: divX, Y: 5})
	if a.drag == nil {
		t.Fatal("clicking the divider did not start a drag")
	}
	a.handleMouse(uv.MouseMotionEvent{X: divX + 5, Y: 5}, uv.Mouse{X: divX + 5, Y: 5})

	if got := a.rects[1].W; got != before+5 {
		t.Fatalf("left pane = %d columns after a 5-cell drag, want %d", got, before+5)
	}
	if got := a.rects[2].W; got != 60-layout.DividerW-(before+5) {
		t.Fatalf("right pane = %d columns, want %d", got, 60-layout.DividerW-(before+5))
	}

	a.handleMouse(uv.MouseReleaseEvent{X: divX + 5, Y: 5}, uv.Mouse{X: divX + 5, Y: 5})
	if a.drag != nil {
		t.Error("releasing did not end the drag")
	}
}

// Dragging is absolute, not incremental: going out and coming back must land
// exactly where it started, with no accumulated drift.
func TestDragIsAbsoluteAndReversible(t *testing.T) {
	a := dragApp(t, 60, 12)
	before := a.rects[1].W
	divX := a.divs[0].Rect.X

	a.handleMouse(uv.MouseClickEvent{X: divX, Y: 5}, uv.Mouse{X: divX, Y: 5})
	for _, dx := range []int{3, 7, 12, 4, 0} {
		a.handleMouse(uv.MouseMotionEvent{X: divX + dx, Y: 5}, uv.Mouse{X: divX + dx, Y: 5})
	}
	if got := a.rects[1].W; got != before {
		t.Fatalf("left pane = %d columns after returning to the start, want %d", got, before)
	}
}

// A pane must never be squeezed below its minimum, however far the pointer goes.
func TestDragStopsAtTheMinimumWidth(t *testing.T) {
	a := dragApp(t, 60, 12)
	divX := a.divs[0].Rect.X

	a.handleMouse(uv.MouseClickEvent{X: divX, Y: 5}, uv.Mouse{X: divX, Y: 5})
	a.handleMouse(uv.MouseMotionEvent{X: 500, Y: 5}, uv.Mouse{X: 500, Y: 5})
	if got := a.rects[2].W; got != layout.MinPaneW {
		t.Errorf("right pane = %d columns, want the minimum %d", got, layout.MinPaneW)
	}

	a.handleMouse(uv.MouseMotionEvent{X: -500, Y: 5}, uv.Mouse{X: -500, Y: 5})
	if got := a.rects[1].W; got != layout.MinPaneW {
		t.Errorf("left pane = %d columns, want the minimum %d", got, layout.MinPaneW)
	}
}
