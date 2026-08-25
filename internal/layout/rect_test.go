package layout

import (
	"reflect"
	"testing"
)

// Cumulative rounding must make the parts sum to the total exactly, with no
// stray column left at the edge.
func TestDistributeSumsToTotal(t *testing.T) {
	cases := []struct {
		total  int
		ratios []int
		want   []int
	}{
		{100, []int{1, 1, 1}, []int{33, 33, 34}},
		{10, []int{3, 1}, []int{7, 3}},
		{7, []int{1, 1}, []int{3, 4}},
		{5, []int{1}, []int{5}},
	}
	for _, c := range cases {
		got := Distribute(c.total, c.ratios)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Distribute(%d, %v) = %v, want %v", c.total, c.ratios, got, c.want)
		}
		sum := 0
		for _, v := range got {
			sum += v
		}
		if sum != c.total {
			t.Errorf("Distribute(%d, %v) sums to %d", c.total, c.ratios, sum)
		}
	}
}

func TestComputeSingleLeafFillsTheArea(t *testing.T) {
	got := Compute(leaf(1), Rect{X: 0, Y: 0, W: 80, H: 24})
	want := map[PaneID]Rect{1: {X: 0, Y: 0, W: 80, H: 24}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Compute = %v, want %v", got, want)
	}
}

// A horizontal split spends DividerW columns on each divider. Two columns
// rather than one: a terminal cell is about twice as tall as it is wide, so a
// two-column vertical bar reads as thick as a one-row horizontal one — and it
// doubles the target a pointer has to hit in order to drag it.
func TestComputeHorizontalSplitReservesTheDividerColumns(t *testing.T) {
	root := &Node{
		Kind:        KindSplit,
		Orientation: Horizontal,
		Ratios:      []int{1, 1},
		Children:    []*Node{leaf(1), leaf(2)},
	}
	got := Compute(root, Rect{X: 0, Y: 0, W: 22, H: 10})
	want := map[PaneID]Rect{
		1: {X: 0, Y: 0, W: 10, H: 10},
		2: {X: 12, Y: 0, W: 10, H: 10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Compute = %v, want %v", got, want)
	}
}

func TestComputeVerticalSplitReservesADividerRow(t *testing.T) {
	root := &Node{
		Kind:        KindSplit,
		Orientation: Vertical,
		Ratios:      []int{1, 1},
		Children:    []*Node{leaf(1), leaf(2)},
	}
	got := Compute(root, Rect{X: 0, Y: 0, W: 40, H: 11})
	want := map[PaneID]Rect{
		1: {X: 0, Y: 0, W: 40, H: 5},
		2: {X: 0, Y: 6, W: 40, H: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Compute = %v, want %v", got, want)
	}
}

// Only the active child of a stack gets a rectangle; the others are hidden.
func TestComputeStackOnlyPlacesTheActiveChild(t *testing.T) {
	root := &Node{
		Kind:     KindStack,
		Active:   1,
		Children: []*Node{leaf(1), leaf(2), leaf(3)},
	}
	got := Compute(root, Rect{X: 2, Y: 3, W: 20, H: 8})
	want := map[PaneID]Rect{2: {X: 2, Y: 3, W: 20, H: 8}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Compute = %v, want %v", got, want)
	}
}

func TestDividersReportsOnePositionPerGap(t *testing.T) {
	root := &Node{
		Kind:        KindSplit,
		Orientation: Horizontal,
		Ratios:      []int{1, 1, 1},
		Children:    []*Node{leaf(1), leaf(2), leaf(3)},
	}
	got := Dividers(root, Rect{X: 0, Y: 0, W: 34, H: 10})
	if len(got) != 2 {
		t.Fatalf("Dividers = %d entries, want 2", len(got))
	}
	for i, d := range got {
		if d.Rect.W != DividerW || d.Rect.H != 10 {
			t.Errorf("divider %d = %+v, want a %dx10 column", i, d.Rect, DividerW)
		}
		if d.Index != i {
			t.Errorf("divider %d has Index %d", i, d.Index)
		}
	}
}
