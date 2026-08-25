package app

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
)

func TestPaneAtFindsTheRectangleUnderThePointer(t *testing.T) {
	rects := fixture()
	cases := []struct {
		x, y int
		want layout.PaneID
	}{
		{0, 0, 1},
		{39, 9, 1},
		{41, 5, 2},
		{40, 5, 0}, // the divider column belongs to no pane
		{10, 15, 3},
		{200, 200, 0},
	}
	for _, c := range cases {
		if got := paneAt(rects, c.x, c.y); got != c.want {
			t.Errorf("paneAt(%d, %d) = %d, want %d", c.x, c.y, got, c.want)
		}
	}
}

func TestDividerAtFindsTheStrip(t *testing.T) {
	divs := []layout.DividerRect{
		{Rect: layout.Rect{X: 40, Y: 0, W: 1, H: 10}, Index: 0},
		{Rect: layout.Rect{X: 0, Y: 10, W: 81, H: 1}, Index: 0},
	}
	if got := dividerAt(divs, 40, 3); got != 0 {
		t.Errorf("dividerAt(40, 3) = %d, want 0", got)
	}
	if got := dividerAt(divs, 5, 10); got != 1 {
		t.Errorf("dividerAt(5, 10) = %d, want 1", got)
	}
	if got := dividerAt(divs, 5, 3); got != -1 {
		t.Errorf("dividerAt(5, 3) = %d, want -1", got)
	}
}

// Coordinates handed to a guest must be relative to its own top-left corner,
// and the event must keep its concrete type so the guest sees the same kind of
// event it would receive from a real terminal.
func TestTranslateRebasesCoordinatesAndKeepsTheType(t *testing.T) {
	in := uv.MouseClickEvent{X: 45, Y: 7, Button: uv.MouseLeft}
	out := translate(in, 41, 3)
	m := out.Mouse()
	if m.X != 4 || m.Y != 4 {
		t.Fatalf("translate = (%d, %d), want (4, 4)", m.X, m.Y)
	}
	if _, ok := out.(uv.MouseClickEvent); !ok {
		t.Fatalf("translate returned %T, want uv.MouseClickEvent", out)
	}
	if m.Button != uv.MouseLeft {
		t.Fatalf("button = %v, want MouseLeft", m.Button)
	}

	for _, ev := range []uv.MouseEvent{
		uv.MouseReleaseEvent{X: 45, Y: 7},
		uv.MouseWheelEvent{X: 45, Y: 7},
		uv.MouseMotionEvent{X: 45, Y: 7},
	} {
		got := translate(ev, 41, 3)
		if gm := got.Mouse(); gm.X != 4 || gm.Y != 4 {
			t.Errorf("translate(%T) = (%d, %d), want (4, 4)", ev, gm.X, gm.Y)
		}
		if _, same := got.(uv.MouseEvent); !same {
			t.Errorf("translate(%T) returned %T", ev, got)
		}
	}
}
