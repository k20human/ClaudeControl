package app

import (
	"testing"

	"claudecontrol/internal/layout"
)

// Three panes: 1 and 2 side by side on top, 3 spanning the bottom.
func fixture() map[layout.PaneID]layout.Rect {
	return map[layout.PaneID]layout.Rect{
		1: {X: 0, Y: 0, W: 40, H: 10},
		2: {X: 41, Y: 0, W: 40, H: 10},
		3: {X: 0, Y: 11, W: 81, H: 10},
	}
}

func TestNearestPicksTheNeighbourInEachDirection(t *testing.T) {
	rects := fixture()
	cases := []struct {
		from layout.PaneID
		dir  Direction
		want layout.PaneID
	}{
		{1, Right, 2},
		{2, Left, 1},
		{1, Down, 3},
		{3, Up, 1},
	}
	for _, c := range cases {
		if got := nearest(rects, c.from, c.dir); got != c.want {
			t.Errorf("nearest(%d, %v) = %d, want %d", c.from, c.dir, got, c.want)
		}
	}
}

// With nothing in that direction, focus must not move.
func TestNearestKeepsFocusWhenThereIsNoNeighbour(t *testing.T) {
	rects := fixture()
	if got := nearest(rects, 1, Left); got != 1 {
		t.Errorf("nearest(1, Left) = %d, want 1", got)
	}
	if got := nearest(rects, 3, Down); got != 3 {
		t.Errorf("nearest(3, Down) = %d, want 3", got)
	}
}
