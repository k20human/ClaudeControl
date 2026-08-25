package app

import (
	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
)

// dragState records an in-flight divider drag.
type dragState struct {
	parent *layout.Node
	index  int
	horiz  bool
	origin int   // pointer position, along the split axis, where the drag began
	ratios []int // the parent's ratios as they were when the drag began
	total  int   // cells the parent actually distributes, dividers excluded
}

// paneAt returns the pane under the pointer, or 0 for chrome and empty space.
func paneAt(rects map[layout.PaneID]layout.Rect, x, y int) layout.PaneID {
	for id, r := range rects {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return id
		}
	}
	return 0
}

// dividerAt returns the index into divs of the strip under the pointer, or -1.
func dividerAt(divs []layout.DividerRect, x, y int) int {
	for i, d := range divs {
		if x >= d.Rect.X && x < d.Rect.X+d.Rect.W && y >= d.Rect.Y && y < d.Rect.Y+d.Rect.H {
			return i
		}
	}
	return -1
}

// translate rebases a mouse event onto a pane's local origin, preserving its
// concrete type so the guest sees the same kind of event.
func translate(ev uv.MouseEvent, dx, dy int) uv.MouseEvent {
	m := ev.Mouse()
	m.X -= dx
	m.Y -= dy
	switch ev.(type) {
	case uv.MouseClickEvent:
		return uv.MouseClickEvent(m)
	case uv.MouseReleaseEvent:
		return uv.MouseReleaseEvent(m)
	case uv.MouseWheelEvent:
		return uv.MouseWheelEvent(m)
	case uv.MouseMotionEvent:
		return uv.MouseMotionEvent(m)
	}
	return ev
}

// handleMouse routes one mouse event: chrome first, then focus-stealing on an
// unfocused pane, then forwarding to the focused guest.
func (a *App) handleMouse(ev uv.MouseEvent, m uv.Mouse) {
	if a.drag != nil {
		a.continueDrag(ev, m)
		return
	}

	if _, isClick := ev.(uv.MouseClickEvent); isClick {
		if i := dividerAt(a.divs, m.X, m.Y); i >= 0 {
			a.beginDrag(i, m)
			return
		}
	}

	id := paneAt(a.rects, m.X, m.Y)
	if id == 0 {
		return
	}
	if id != a.focus {
		// The first click focuses and is swallowed, so moving to another pane
		// can never trigger something inside the pane you are moving to.
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			a.setFocus(id)
		}
		return
	}

	r := a.rects[id]
	if mod, ok := a.modules[id].(module.Inputter); ok {
		mod.Mouse(translate(ev, r.X, r.Y))
	}
}

// beginDrag records what a divider drag needs in order to adjust ratios.
func (a *App) beginDrag(i int, m uv.Mouse) {
	d := a.divs[i]
	horiz := d.Rect.W == 1
	origin, total := m.Y, d.Area.H
	if horiz {
		origin, total = m.X, d.Area.W
	}
	// The split distributes everything except the one-cell divider strips.
	total -= len(d.Parent.Children) - 1
	if total < 1 {
		return
	}
	a.drag = &dragState{
		parent: d.Parent,
		index:  d.Index,
		horiz:  horiz,
		origin: origin,
		ratios: append([]int(nil), d.Parent.Ratios...),
		total:  total,
	}
}

// continueDrag moves cells between the two siblings the divider separates, and
// ends the drag on release.
func (a *App) continueDrag(ev uv.MouseEvent, m uv.Mouse) {
	d := a.drag
	if _, released := ev.(uv.MouseReleaseEvent); released {
		a.drag = nil
	}

	pos := m.Y
	if d.horiz {
		pos = m.X
	}
	sum := 0
	for _, r := range d.ratios {
		sum += r
	}
	if sum <= 0 {
		return
	}
	// Convert the movement in cells into ratio units so the divider tracks the
	// pointer rather than drifting away from it.
	shift := (pos - d.origin) * sum / d.total

	left := d.ratios[d.index] + shift
	right := d.ratios[d.index+1] - shift
	if left < 1 || right < 1 {
		return
	}
	d.parent.Ratios[d.index] = left
	d.parent.Ratios[d.index+1] = right
	a.relayout()
}
