package session

import (
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
type View struct{ offset int }

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
}
