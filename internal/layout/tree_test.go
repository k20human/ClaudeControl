package layout

import "testing"

func leaf(id PaneID) *Node { return &Node{Kind: KindLeaf, PaneID: id} }

func TestSplitRootLeafWrapsItInASplit(t *testing.T) {
	root, err := Split(leaf(1), 1, leaf(2), Horizontal)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if root.Kind != KindSplit || root.Orientation != Horizontal {
		t.Fatalf("root = %+v, want a horizontal split", root)
	}
	if got := Leaves(root); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("Leaves = %v, want [1 2]", got)
	}
	if got := root.Ratios; len(got) != 2 || got[0] != got[1] {
		t.Fatalf("Ratios = %v, want two equal ratios", got)
	}
}

// A split in the same orientation must add a sibling rather than nest, and it
// must take its share from the target alone so the other siblings keep their
// size.
func TestSplitSameOrientationAddsSiblingAndOnlySplitsTheTarget(t *testing.T) {
	root := &Node{
		Kind:        KindSplit,
		Orientation: Horizontal,
		Ratios:      []int{60, 40},
		Children:    []*Node{leaf(1), leaf(2)},
	}
	root, err := Split(root, 1, leaf(3), Horizontal)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(root.Children) != 3 {
		t.Fatalf("children = %d, want 3 (sibling, not nested)", len(root.Children))
	}
	if got := Leaves(root); got[0] != 1 || got[1] != 3 || got[2] != 2 {
		t.Fatalf("Leaves = %v, want [1 3 2]", got)
	}
	if root.Ratios[0] != 30 || root.Ratios[1] != 30 || root.Ratios[2] != 40 {
		t.Fatalf("Ratios = %v, want [30 30 40]", root.Ratios)
	}
}

func TestSplitDifferentOrientationNests(t *testing.T) {
	root := &Node{
		Kind:        KindSplit,
		Orientation: Horizontal,
		Ratios:      []int{50, 50},
		Children:    []*Node{leaf(1), leaf(2)},
	}
	root, err := Split(root, 2, leaf(3), Vertical)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(root.Children) != 2 {
		t.Fatalf("children = %d, want 2", len(root.Children))
	}
	nested := root.Children[1]
	if nested.Kind != KindSplit || nested.Orientation != Vertical {
		t.Fatalf("child[1] = %+v, want a vertical split", nested)
	}
}

func TestRemoveCollapsesSingleChildSplit(t *testing.T) {
	root := &Node{
		Kind:        KindSplit,
		Orientation: Horizontal,
		Ratios:      []int{50, 50},
		Children:    []*Node{leaf(1), leaf(2)},
	}
	root, err := Remove(root, 2)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if root.Kind != KindLeaf || root.PaneID != 1 {
		t.Fatalf("root = %+v, want leaf 1", root)
	}
}

func TestRemoveRedistributesRatiosProportionally(t *testing.T) {
	root := &Node{
		Kind:        KindSplit,
		Orientation: Horizontal,
		Ratios:      []int{20, 30, 50},
		Children:    []*Node{leaf(1), leaf(2), leaf(3)},
	}
	root, err := Remove(root, 2)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// 20:50 keeps its proportion; only the absolute values are rescaled.
	if len(root.Ratios) != 2 || root.Ratios[0]*50 != root.Ratios[1]*20 {
		t.Fatalf("Ratios = %v, want 20:50 proportion preserved", root.Ratios)
	}
}

func TestRemoveLastLeafReturnsNilRoot(t *testing.T) {
	root, err := Remove(leaf(1), 1)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if root != nil {
		t.Fatalf("root = %+v, want nil", root)
	}
}

func TestRemoveUnknownPaneIsAnError(t *testing.T) {
	if _, err := Remove(leaf(1), 99); err == nil {
		t.Fatal("Remove(99) = nil error, want an error")
	}
}

// Swapping exchanges two panes without touching the shape of the tree. That is
// what makes it predictable: the arrangement you built stays, only the contents
// cross over.
func TestMoveSwapExchangesTwoPanesInPlace(t *testing.T) {
	root := &Node{
		Kind:        KindSplit,
		Orientation: Horizontal,
		Ratios:      []int{60, 40},
		Children:    []*Node{leaf(1), leaf(2)},
	}
	got, err := Move(root, 1, 2, SideSwap)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if ids := Leaves(got); len(ids) != 2 || ids[0] != 2 || ids[1] != 1 {
		t.Fatalf("Leaves = %v, want [2 1]", ids)
	}
	if got.Ratios[0] != 60 || got.Ratios[1] != 40 {
		t.Errorf("Ratios = %v, want the shape untouched", got.Ratios)
	}
}

// Doing it again undoes it, which is what lets someone try a swap and change
// their mind without an undo stack.
func TestMoveSwapIsItsOwnInverse(t *testing.T) {
	root := &Node{
		Kind:        KindSplit,
		Orientation: Horizontal,
		Ratios:      []int{1, 1},
		Children: []*Node{
			leaf(1),
			{Kind: KindSplit, Orientation: Vertical, Ratios: []int{1, 1},
				Children: []*Node{leaf(2), leaf(3)}},
		},
	}
	before := Leaves(root)
	root, err := Move(root, 1, 3, SideSwap)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	root, err = Move(root, 3, 1, SideSwap)
	if err != nil {
		t.Fatalf("Move back: %v", err)
	}
	after := Leaves(root)
	if len(before) != len(after) {
		t.Fatalf("Leaves = %v, want %v", after, before)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("Leaves = %v, want %v", after, before)
		}
	}
}

// The four sides put the pane beside its target, on the side you dropped it.
func TestMoveToASidePlacesThePaneThere(t *testing.T) {
	cases := []struct {
		side Side
		want []PaneID // visual order afterwards
		axis Orientation
	}{
		{SideRight, []PaneID{2, 3, 1}, Horizontal},
		{SideLeft, []PaneID{2, 1, 3}, Horizontal},
		{SideBottom, []PaneID{2, 3, 1}, Vertical},
		{SideTop, []PaneID{2, 1, 3}, Vertical},
	}
	for _, c := range cases {
		// 1 and 2 side by side, 3 below them. Pane 1 is moved onto pane 3.
		root := &Node{
			Kind: KindSplit, Orientation: Vertical, Ratios: []int{1, 1},
			Children: []*Node{
				{Kind: KindSplit, Orientation: Horizontal, Ratios: []int{1, 1},
					Children: []*Node{leaf(1), leaf(2)}},
				leaf(3),
			},
		}
		got, err := Move(root, 1, 3, c.side)
		if err != nil {
			t.Errorf("Move(%v): %v", c.side, err)
			continue
		}
		ids := Leaves(got)
		if len(ids) != len(c.want) {
			t.Errorf("Move(%v) gave %v, want %v", c.side, ids, c.want)
			continue
		}
		for i := range ids {
			if ids[i] != c.want[i] {
				t.Errorf("Move(%v) gave %v, want %v", c.side, ids, c.want)
				break
			}
		}
	}
}

// Taking a pane out of a two-way split leaves nothing to divide, so the split
// collapses rather than lingering with one child.
func TestMovingOutOfATwoWaySplitCollapsesIt(t *testing.T) {
	root := &Node{
		Kind: KindSplit, Orientation: Vertical, Ratios: []int{1, 1},
		Children: []*Node{
			{Kind: KindSplit, Orientation: Horizontal, Ratios: []int{1, 1},
				Children: []*Node{leaf(1), leaf(2)}},
			leaf(3),
		},
	}
	got, err := Move(root, 1, 3, SideRight)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	// Pane 2 was alone in its split; that split must be gone.
	if got.Children[0].Kind != KindLeaf || got.Children[0].PaneID != 2 {
		t.Fatalf("first child = %+v, want the surviving leaf 2", got.Children[0])
	}
}

func TestMovingAPaneOntoItselfChangesNothing(t *testing.T) {
	root := &Node{
		Kind: KindSplit, Orientation: Horizontal, Ratios: []int{1, 1},
		Children: []*Node{leaf(1), leaf(2)},
	}
	for _, side := range []Side{SideSwap, SideLeft, SideRight, SideTop, SideBottom} {
		got, err := Move(root, 1, 1, side)
		if err != nil {
			t.Errorf("Move(%v) onto itself: %v", side, err)
			continue
		}
		if ids := Leaves(got); len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
			t.Errorf("Move(%v) onto itself gave %v, want [1 2]", side, ids)
		}
	}
}

func TestMovingTheOnlyPaneIsAnError(t *testing.T) {
	if _, err := Move(leaf(1), 1, 2, SideRight); err == nil {
		t.Fatal("moving the only pane was allowed")
	}
}

func TestMovingToAnUnknownTargetIsAnError(t *testing.T) {
	root := &Node{
		Kind: KindSplit, Orientation: Horizontal, Ratios: []int{1, 1},
		Children: []*Node{leaf(1), leaf(2)},
	}
	if _, err := Move(root, 1, 99, SideRight); err == nil {
		t.Fatal("moving onto a pane that does not exist was allowed")
	}
	if _, err := Move(root, 99, 1, SideRight); err == nil {
		t.Fatal("moving a pane that does not exist was allowed")
	}
}
