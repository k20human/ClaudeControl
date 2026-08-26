package app

import (
	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
)

// dragState records an in-flight divider drag.
//
// Sizes are kept in cells, not in ratio units. Ratios are relative, so
// converting a pointer movement into them means dividing by their sum: with
// ratios [1 1] over sixty columns that quotient is zero until the pointer has
// travelled thirty cells, and the divider simply does not move. Cells are what
// the pointer speaks, and a split's ratios can hold cell counts just as well.
type dragState struct {
	parent *layout.Node
	index  int
	horiz  bool
	origin int   // pointer position, along the split axis, where the drag began
	cells  []int // each child's size in cells when the drag began
	min    int   // smallest a child may become, along the split axis
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
//
// The test rectangle is grown by GrabPad along the split axis. A one-column bar
// is comfortable to look at and miserable to hit, so the visible bar and the
// target it offers are deliberately different sizes.
func dividerAt(divs []layout.DividerRect, x, y int) int {
	for i, d := range divs {
		r := d.Rect
		if d.Parent != nil && d.Parent.Orientation == layout.Horizontal {
			r.X -= layout.GrabPad
			r.W += 2 * layout.GrabPad
		} else {
			r.Y -= layout.GrabPad
			r.H += 2 * layout.GrabPad
		}
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
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

	a.pointerX, a.pointerY = m.X, m.Y

	// A panel takes the whole screen until it is dismissed. Its own buttons
	// come first; a click anywhere else closes it without acting, which for
	// the quit confirmation means staying.
	if a.overlay != overlayNone {
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			if i := buttonAt(a.panelButtons, m.X, m.Y); i >= 0 {
				a.panelButtons[i].run(a)
				return
			}
			a.dismissOverlay(false)
		}
		return
	}

	// The session list takes the pointer too: a click outside it closes it,
	// and one on the status bar still reaches the buttons.
	if a.sessionPanel != nil {
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			if i := buttonAt(a.buttons, m.X, m.Y); i >= 0 {
				a.buttons[i].run(a)
				return
			}
			a.toggleSessionPanel()
		}
		return
	}

	a.hoverDiv = dividerAt(a.divs, m.X, m.Y)
	a.hoverBtn = buttonAt(a.buttons, m.X, m.Y)

	if i := a.hoverBtn; i >= 0 {
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			a.buttons[i].run(a)
		}
		return
	}

	if _, isClick := ev.(uv.MouseClickEvent); isClick && a.hoverDiv >= 0 {
		a.beginDrag(a.hoverDiv, m)
		return
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

// beginDrag freezes the sizes a drag will work from.
func (a *App) beginDrag(i int, m uv.Mouse) {
	d := a.divs[i]
	horiz := d.Parent.Orientation == layout.Horizontal
	origin, span, min, thick := m.Y, d.Area.H, layout.MinPaneH, layout.DividerH
	if horiz {
		origin, span, min, thick = m.X, d.Area.W, layout.MinPaneW, layout.DividerW
	}
	// The split distributes everything except the divider strips.
	span -= (len(d.Parent.Children) - 1) * thick
	if span < 2*min {
		return
	}
	a.drag = &dragState{
		parent: d.Parent,
		index:  d.Index,
		horiz:  horiz,
		origin: origin,
		cells:  layout.Distribute(span, d.Parent.Ratios),
		min:    min,
	}
}

// continueDrag moves cells between the two siblings the divider separates, and
// ends the drag on release.
//
// Every move is computed from the sizes frozen at the start rather than from
// the previous position, so the divider tracks the pointer exactly and a drag
// that wanders and comes back lands where it began.
func (a *App) continueDrag(ev uv.MouseEvent, m uv.Mouse) {
	d := a.drag
	if _, released := ev.(uv.MouseReleaseEvent); released {
		a.drag = nil
	}

	pos := m.Y
	if d.horiz {
		pos = m.X
	}
	delta := pos - d.origin

	// Clamp so neither neighbour is squeezed below the minimum.
	if lo := d.min - d.cells[d.index]; delta < lo {
		delta = lo
	}
	if hi := d.cells[d.index+1] - d.min; delta > hi {
		delta = hi
	}

	// Ratios are relative, so writing cell counts into them is exact: their
	// sum is the span, and Distribute over that span returns them unchanged.
	next := append([]int(nil), d.cells...)
	next[d.index] += delta
	next[d.index+1] -= delta
	copy(d.parent.Ratios, next)
	a.relayout()
}
