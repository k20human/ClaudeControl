package app

import (
	"testing"

	"claudecontrol/internal/layout"
)

// The five zones. A diamond in the middle swaps; the four triangles around it
// split. Getting the geometry wrong means the pane lands somewhere other than
// where the preview said it would.
func TestDropZoneFollowsThePointer(t *testing.T) {
	r := layout.Rect{X: 10, Y: 10, W: 40, H: 20}
	cases := []struct {
		name string
		x, y int
		want layout.Side
	}{
		{"dead centre", 30, 20, layout.SideSwap},
		{"just left of centre", 26, 20, layout.SideSwap},
		{"far left", 11, 20, layout.SideLeft},
		{"far right", 48, 20, layout.SideRight},
		{"top edge", 30, 11, layout.SideTop},
		{"bottom edge", 30, 28, layout.SideBottom},
	}
	for _, c := range cases {
		if got := dropZone(r, c.x, c.y); got != c.want {
			t.Errorf("%s: dropZone(%d,%d) = %v, want %v", c.name, c.x, c.y, got, c.want)
		}
	}
}

// A corner is ambiguous by construction, so the rule has to be stated: the
// axis you are proportionally further along wins. A wide pane is further along
// its vertical axis long before its horizontal one.
func TestACornerResolvesToTheDeeperAxis(t *testing.T) {
	// 40 wide, 20 tall. The top-left corner is 10% across and 10% down, which
	// in normalised terms is 0.8 out on x and 0.8 out on y — a tie broken by
	// the rule, not by chance.
	r := layout.Rect{X: 0, Y: 0, W: 40, H: 20}
	if got := dropZone(r, 1, 9); got != layout.SideLeft {
		t.Errorf("a point far out on x and near the middle on y gave %v, want left", got)
	}
	if got := dropZone(r, 19, 1); got != layout.SideTop {
		t.Errorf("a point near the middle on x and far out on y gave %v, want top", got)
	}
}

// The preview must cover the region the pane is actually going to occupy, or
// it is not a preview.
func TestPreviewRectMatchesTheZone(t *testing.T) {
	r := layout.Rect{X: 10, Y: 10, W: 40, H: 20}
	cases := []struct {
		side layout.Side
		want layout.Rect
	}{
		{layout.SideSwap, r},
		{layout.SideLeft, layout.Rect{X: 10, Y: 10, W: 20, H: 20}},
		{layout.SideRight, layout.Rect{X: 30, Y: 10, W: 20, H: 20}},
		{layout.SideTop, layout.Rect{X: 10, Y: 10, W: 40, H: 10}},
		{layout.SideBottom, layout.Rect{X: 10, Y: 20, W: 40, H: 10}},
	}
	for _, c := range cases {
		if got := previewRect(r, c.side); got != c.want {
			t.Errorf("previewRect(%v) = %+v, want %+v", c.side, got, c.want)
		}
	}
}

// A drag that ends where it started must change nothing, and neither must one
// that is cancelled.
func TestADragOntoItselfOrCancelledChangesNothing(t *testing.T) {
	a := dragApp(t, 80, 24)
	before := layout.Leaves(a.root)

	a.beginPaneDrag(1)
	a.updatePaneDrag(a.rects[1].X+1, a.rects[1].Y+1)
	a.finishPaneDrag()
	if got := layout.Leaves(a.root); !sameIDs(got, before) {
		t.Errorf("Leaves = %v after dropping a pane on itself, want %v", got, before)
	}

	a.beginPaneDrag(1)
	a.updatePaneDrag(a.rects[2].X+1, a.rects[2].Y+1)
	a.cancelPaneDrag()
	if got := layout.Leaves(a.root); !sameIDs(got, before) {
		t.Errorf("Leaves = %v after cancelling, want %v", got, before)
	}
	if a.paneDrag != nil {
		t.Error("the drag is still in progress after cancelling")
	}
}

// The gesture end to end, on the tree: pick up the left pane, drop it on the
// right half of the right pane, and it should end up rightmost.
func TestDraggingAPaneToTheRightOfAnotherMovesIt(t *testing.T) {
	a := dragApp(t, 80, 24)
	target := a.rects[2]

	a.beginPaneDrag(1)
	a.updatePaneDrag(target.X+target.W-2, target.Y+target.H/2)
	if a.paneDrag == nil || a.paneDrag.side != layout.SideRight {
		t.Fatalf("the pointer at the right edge chose %v", a.paneDrag)
	}
	a.finishPaneDrag()

	if got := layout.Leaves(a.root); len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("Leaves = %v, want [2 1]", got)
	}
	if a.paneDrag != nil {
		t.Error("the drag did not end")
	}
}

func sameIDs(a, b []layout.PaneID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Dropping on the middle of a pane swaps the two, so each ends up in the
// other's place. The two halves of an odd-width split are not the same size,
// which is why this checks the rectangles rather than assuming a swap leaves
// the geometry alone — it does not, and a pane that changes size relies on
// session.Resize repainting it.
func TestDroppingOnTheMiddleSwapsTheTwoPanes(t *testing.T) {
	a := dragApp(t, 81, 24)
	one, two := a.rects[1], a.rects[2]
	if one == two {
		t.Fatalf("the two panes start out identical (%v); the test proves nothing", one)
	}

	a.beginPaneDrag(1)
	a.updatePaneDrag(two.X+two.W/2, two.Y+two.H/2)
	if a.paneDrag.side != layout.SideSwap {
		t.Fatalf("the middle of a pane chose %v, want swap", a.paneDrag.side)
	}
	a.finishPaneDrag()

	if a.rects[1] != two {
		t.Errorf("pane 1 is at %v after the swap, want pane 2's old place %v", a.rects[1], two)
	}
	if a.rects[2] != one {
		t.Errorf("pane 2 is at %v after the swap, want pane 1's old place %v", a.rects[2], one)
	}
}
