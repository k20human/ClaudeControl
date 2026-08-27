package session

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// WheelStep is how many lines one notch of the wheel moves. Three is what
// terminals settle on: one line is too slow to be worth the gesture, a whole
// screen loses your place.
const WheelStep = 3

// View is a window onto a session, which is usually the live screen and is
// sometimes further back.
//
// It exists because a guest on the normal screen does not scroll: it prints,
// and what it printed goes into the scrollback. In a terminal you reach that
// with the terminal's own scrollbar. Hosting the guest takes that away, so the
// wheel has to reach it here instead.
type View struct {
	offset int
	sel    selection
}

// point is a place in the session's whole output, screen and history together.
// Lines below the history length are on the live screen.
type point struct{ line, col int }

// selection is a range of that output.
//
// It is kept in those absolute terms rather than in rows of the window,
// because the window moves: scrolling while text is selected must leave the
// selection on the text it was on, not on whatever has slid under it.
type selection struct {
	active   bool
	dragging bool
	from, to point
	// pressed is where the button went down, kept so a drag can begin from
	// there once it is clear that it is a drag and not a click.
	pressed point
	// forwarded records that the guest was told about the press, and so has
	// to be told about a release even though we kept the drag.
	forwarded bool
}

// absLine turns a row of the window into a line of the whole output.
func (v *View) absLine(s *Session, row int) int {
	return s.History() - v.offset + row
}

// Press records where the button went down. Nothing is selected yet: a press
// that is followed by a release and no movement is a click, and the guest is
// entitled to it.
func (v *View) Press(s *Session, x, y int, forwarded bool) {
	v.sel.pressed = point{line: v.absLine(s, y), col: x}
	v.sel.forwarded = forwarded
	v.sel.dragging = false
}

// Drag extends the selection, starting it on the first movement.
func (v *View) Drag(s *Session, x, y int) {
	if !v.sel.dragging {
		v.sel.dragging, v.sel.active = true, true
		v.sel.from = v.sel.pressed
	}
	v.sel.to = point{line: v.absLine(s, y), col: x}
}

// Release ends a drag and reports whether the gesture was a selection. A
// selection of nothing is not one: dragging back to where you started clears
// it rather than leaving an invisible empty range behind.
func (v *View) Release() bool {
	was := v.sel.dragging
	v.sel.dragging = false
	if was && v.sel.from == v.sel.to {
		v.sel.active = false
	}
	return was
}

// Dragging reports whether a drag is in progress.
func (v *View) Dragging() bool { return v.sel.dragging }

// PressForwarded reports whether the guest was told about the press that began
// this gesture, and so is owed a release.
func (v *View) PressForwarded() bool { return v.sel.forwarded }

// HasSelection reports whether there is text selected.
func (v *View) HasSelection() bool { return v.sel.active }

// ClearSelection drops it.
func (v *View) ClearSelection() { v.sel = selection{} }

// ordered returns the selection with its ends the right way round, since you
// may drag upwards.
func (v *View) ordered() (point, point) {
	a, b := v.sel.from, v.sel.to
	if b.line < a.line || (b.line == a.line && b.col < a.col) {
		a, b = b, a
	}
	return a, b
}

// Selected reports whether a line and column are inside the selection.
func (v *View) Selected(line, col int) bool {
	if !v.sel.active {
		return false
	}
	a, b := v.ordered()
	switch {
	case line < a.line || line > b.line:
		return false
	case line == a.line && line == b.line:
		return col >= a.col && col <= b.col
	case line == a.line:
		return col >= a.col
	case line == b.line:
		return col <= b.col
	}
	return true
}

// Text is the selected text, one line per line, with the trailing blanks of
// each line dropped — they are padding rather than something you selected.
func (v *View) Text(s *Session, width int) string {
	if !v.sel.active || s == nil {
		return ""
	}
	a, b := v.ordered()
	var out []string
	for line := a.line; line <= b.line; line++ {
		from, to := 0, width-1
		if line == a.line {
			from = a.col
		}
		if line == b.line {
			to = b.col
		}
		out = append(out, strings.TrimRight(s.LineText(line, from, to), " "))
	}
	return strings.Join(out, "\n")
}

// Offset is how many lines back the view is, zero being the live screen.
func (v *View) Offset() int { return v.offset }

// Reset returns to the bottom. Typing does this, the way a terminal jumps back
// to the prompt when you press a key: what you type appears where the cursor
// is, and you should be looking at it.
func (v *View) Reset() { v.offset = 0 }

// Wheel moves the view and reports whether it took the event.
//
// It declines when the guest is on the alternate screen. Such a guest is
// drawing its own view — a pager, an editor, a full-screen picker — and has no
// history behind it, since nothing has scrolled off. The wheel is its own, and
// forwarding it is the only thing that could be right.
func (v *View) Wheel(s *Session, up bool) bool {
	if s == nil || s.AltScreen() {
		return false
	}
	if up {
		v.offset += WheelStep
		if max := s.History(); v.offset > max {
			v.offset = max
		}
		return true
	}
	v.offset -= WheelStep
	if v.offset < 0 {
		v.offset = 0
	}
	return true
}

// Draw paints the session through this view.
func (v *View) Draw(s *Session, scr uv.Screen, area uv.Rectangle) {
	if s == nil {
		return
	}
	// History is trimmed as it grows past its cap, so an offset that was valid
	// a minute ago can now be past the end.
	if h := s.History(); v.offset > h {
		v.offset = h
	}
	s.DrawScrolled(scr, area, v.offset)
	v.highlight(s, scr, area)
}

// highlight marks the selected cells, after the pane has been drawn.
//
// The attribute is flipped rather than set, so text the guest had already
// reversed still stands out when you select it — the same reason the cursor
// does it.
func (v *View) highlight(s *Session, scr uv.Screen, area uv.Rectangle) {
	if !v.sel.active {
		return
	}
	history := s.History()
	for row := 0; row < area.Dy(); row++ {
		line := history - v.offset + row
		for col := 0; col < area.Dx(); col++ {
			if !v.Selected(line, col) {
				continue
			}
			c := scr.CellAt(area.Min.X+col, area.Min.Y+row)
			if c == nil {
				continue
			}
			cell := *c
			cell.Style.Attrs ^= uv.AttrReverse
			scr.SetCell(area.Min.X+col, area.Min.Y+row, &cell)
		}
	}
}

// SelectedText is what is selected, or empty.
func (v *View) SelectedText(s *Session) string {
	if s == nil {
		return ""
	}
	return v.Text(s, s.Term.Bounds().Dx())
}
