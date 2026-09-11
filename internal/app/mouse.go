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

	// A tab in flight owns the pointer until it lands.
	if a.tabDrag != nil {
		switch ev.(type) {
		case uv.MouseMotionEvent:
			a.updateTabDrag(m.X, m.Y)
		case uv.MouseReleaseEvent:
			a.finishTabDrag()
		}
		return
	}

	// An open menu owns the pointer, the way every menu does.
	if a.menuMouse(ev, m) {
		return
	}

	// The middle button pastes the selection, the way it does everywhere else
	// on this desktop. The terminal would have done it here, before mouse
	// reporting took the button from it.
	if click, isClick := ev.(uv.MouseClickEvent); isClick && uv.Mouse(click).Button == uv.MouseMiddle {
		if id := paneAt(a.rects, m.X, m.Y); id != 0 {
			a.setFocus(id)
			a.pasteSelection()
			return
		}
	}

	// Right-click opens one over a pane. The terminal would have shown its own
	// here, before mouse reporting took the button from it.
	if click, isClick := ev.(uv.MouseClickEvent); isClick && uv.Mouse(click).Button == uv.MouseRight {
		if id := paneAt(a.rects, m.X, m.Y); id != 0 {
			a.setFocus(id)
			a.openMenu(m.X, m.Y)
			return
		}
	}

	// A pane in flight owns the pointer until it lands. Alt is the modifier
	// because terminals commonly claim shift for selection and ctrl for
	// links, and a pane is full-bleed guest content — without a modifier every
	// drag would belong to the guest.
	if a.paneDrag != nil {
		a.updatePaneDrag(m.X, m.Y)
		if _, released := ev.(uv.MouseReleaseEvent); released {
			a.finishPaneDrag()
		}
		return
	}
	if _, isClick := ev.(uv.MouseClickEvent); isClick && m.Mod.Contains(uv.ModAlt) {
		if id := paneAt(a.rects, m.X, m.Y); id != 0 {
			a.setFocus(id)
			a.beginPaneDrag(id)
			return
		}
	}

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

	// The directory panel answers to the pointer: a click on a row opens a
	// session there, one outside closes it.
	if a.openDir != nil {
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			r := a.convPanelRect()
			choices := a.openDirChoices()
			if row := a.openDir.top + m.Y - r.Y - 3; m.Y >= r.Y+3 && row < len(choices) &&
				m.X >= r.X && m.X < r.X+r.W {
				a.openDir.selected = row
				a.openChosenDir()
				return
			}
			a.closeOpenDir()
		}
		if w, isWheel := ev.(uv.MouseWheelEvent); isWheel {
			switch w.Button {
			case uv.MouseWheelUp:
				a.moveOpenDir(-1)
			case uv.MouseWheelDown:
				a.moveOpenDir(1)
			}
		}
		return
	}

	// The conversation search answers to the pointer as well as the keyboard:
	// a click on a result opens it, and one outside closes the panel.
	if a.conv != nil {
		// The wheel walks the results, which is what a wheel over a list is
		// for; the selection carries the view with it.
		if w, isWheel := ev.(uv.MouseWheelEvent); isWheel {
			if w.Button == uv.MouseWheelUp {
				a.scrollConv(-1)
			} else if w.Button == uv.MouseWheelDown {
				a.scrollConv(1)
			}
			return
		}
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			r := a.convPanelRect()
			hits, _, _ := a.conv.results()
			if row := a.conv.top + (m.Y-r.Y-3)/3; m.Y >= r.Y+3 && row < len(hits) &&
				m.X >= r.X && m.X < r.X+r.W {
				a.conv.selected = row
				a.openConversation()
				return
			}
			a.closeConvSearch()
		}
		return
	}

	if a.palette != nil {
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			r := a.palettePanelRect()
			if row := m.Y - r.Y - 3; row >= 0 && row < len(a.paletteVisible()) &&
				m.X >= r.X && m.X < r.X+r.W {
				a.palette.selected = row
				a.paletteChoose()
				return
			}
			a.togglePalette()
		}
		return
	}

	// The menu takes the pointer while it is open. Its own rectangle handles
	// the click; the status bar stays reachable; anything else closes it.
	if a.settingsPanel != nil {
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			if i := a.buttonUnder(m.X, m.Y); i >= 0 {
				a.buttons[i].run(a)
				return
			}
			r := a.settingsPanelRect()
			if m.X >= r.X && m.X < r.X+r.W && m.Y >= r.Y && m.Y < r.Y+r.H {
				_ = a.settingsPanel.ClickAt(m.X, m.Y, uv.Rect(r.X, r.Y, r.W, r.H))
				return
			}
			a.toggleSettingsPanel()
		}
		return
	}

	// The session list takes the pointer too: a click outside it closes it,
	// and one on the status bar still reaches the buttons.
	if a.sessionPanel != nil {
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			if i := a.buttonUnder(m.X, m.Y); i >= 0 {
				a.buttons[i].run(a)
				return
			}
			a.toggleSessionPanel()
		}
		return
	}

	a.hoverDiv = dividerAt(a.divs, m.X, m.Y)
	// The tip is drawn on a row that belongs to a pane, so moving on or off a
	// button has to hand that row back: a full repaint is what does it.
	if over := a.buttonUnder(m.X, m.Y); over != a.hoverBtn {
		a.hoverBtn = over
		a.clearNext = true
		a.Wake()
	}

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
		click, isClick := ev.(uv.MouseClickEvent)
		if !isClick {
			return
		}
		a.setFocus(id)
		// The first click focuses and is swallowed, so moving to another pane
		// can never trigger something inside the pane you are moving to.
		//
		// A tab strip is the exception, because it is not inside the pane: it
		// is chrome, the way the status bar and the dividers are. Having to
		// click a pane before you could pick up one of its tabs was a rule
		// nobody could see, and picking up a tab is what the strip is for.
		if !a.onStripRow(id, uv.Mouse(click).Y) {
			return
		}
	}

	// A click on the title row belongs to the chrome, not to the guest: the
	// pane is already focused by the time we get here, and that is all the
	// title is for.
	r := a.contentRect(id)
	if m.Y < r.Y {
		return
	}
	// A press on a tab's label arms a move. The press is forwarded all the
	// same, so the tab you are dragging is the one you are looking at; if the
	// pointer never leaves the label, that is all it was.
	if click, isClick := ev.(uv.MouseClickEvent); isClick && uv.Mouse(click).Button == uv.MouseLeft {
		a.armTabDrag(id, m.X-r.X, m.Y-r.Y)
	}
	if mod, ok := a.modules[id].(module.Inputter); ok {
		mod.Mouse(translate(ev, r.X, r.Y))
	}
	// After the pane has had the release, because until it has there is no
	// selection to take.
	if rel, released := ev.(uv.MouseReleaseEvent); released &&
		uv.Mouse(rel).Button == uv.MouseLeft {
		a.takeSelection(id)
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
	a.layoutChanged = true
	a.relayout()
}
