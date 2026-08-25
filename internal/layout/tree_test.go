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
