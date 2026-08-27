package tabs

import (
	"image/color"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/indicator"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/render"
)

// Draw paints the strip and the module on screen.
func (m *Module) Draw(scr uv.Screen, area uv.Rectangle) {
	m.drawStrip(scr, area)

	body := uv.Rect(area.Min.X, area.Min.Y+stripRows, area.Dx(), area.Dy()-stripRows)
	if body.Dy() <= 0 {
		return
	}
	if active := m.Active(); active != nil {
		active.Draw(scr, body)
	}
}

// drawStrip paints one label per tab and records where each landed.
//
// A label that does not fit whole is dropped rather than cut: half a name is
// not a shorter name, and a tab you cannot read is a tab you will not click.
// The count of what was dropped is shown instead, so the strip never lies
// about how many tabs there are.
func (m *Module) drawStrip(scr uv.Screen, area uv.Rectangle) {
	render.Fill(scr, uv.Rect(area.Min.X, area.Min.Y, area.Dx(), stripRows), bgStrip)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	// The + is reserved before anything else is laid out, so a strip that is
	// full still lets you open a tab. A button that disappears when you need
	// it is not a button.
	plusW := ansi.StringWidth(plusLabel)
	limit := area.Max.X - plusW

	x, hidden := area.Min.X, 0
	for i, t := range m.tabs {
		label := " " + t.title + " "
		if t.known {
			// The same mark as everywhere else: one meaning per shape.
			label = " " + indicator.Glyph(t.state, now) + " " + t.title + " "
		}
		// The cross is on the tab you are looking at and no other. It saves
		// the width of one on every tab, and it means a stray click cannot
		// close something you were not even reading.
		if i == m.active && len(m.tabs) > 1 {
			label += closeLabel + " "
		}
		w := ansi.StringWidth(label)
		if x+w > limit {
			t.x, t.w, t.closeX = 0, 0, 0
			hidden++
			continue
		}
		fg, bg := color.Color(fgIdle), color.Color(bgStrip)
		switch {
		case i == m.active:
			fg, bg = fgActive, bgActive
		case t.known && t.state != pool.StateIdle:
			// A tab you are not looking at, whose session has something to
			// say, is coloured by what it has to say.
			fg = indicator.Colour(t.state)
		}
		render.Fill(scr, uv.Rect(x, area.Min.Y, w, 1), bg)
		render.Text(scr, x, area.Min.Y, label, fg, bg)
		// Recorded pane-local, because that is how the pointer arrives: the
		// application translates a click into the pane's own coordinates
		// before the module ever sees it.
		t.x, t.w, t.closeX = x-area.Min.X, w, 0
		if i == m.active && len(m.tabs) > 1 {
			t.closeX = x - area.Min.X + w - 1 - ansi.StringWidth(closeLabel)
		}
		x += w
	}

	if hidden > 0 {
		mark := "…" + itoa(hidden)
		if at := limit - ansi.StringWidth(mark); at > x {
			render.Text(scr, at, area.Min.Y, mark, fgWaiting, bgStrip)
		}
	}

	m.plusX = limit - area.Min.X
	// Filled rather than tinted. It was drawn in the same muted grey as an
	// inactive tab, which made the one control in the strip look like the
	// least important thing in it.
	render.Fill(scr, uv.Rect(limit, area.Min.Y, ansi.StringWidth(plusLabel), 1), bgPlus)
	render.Text(scr, limit, area.Min.Y, plusLabel, fgPlus, bgPlus)

	// How far back the tab on screen is, when it is not at the bottom. The
	// pane has no title row to put this in — the strip took it — and without
	// it you would type into a pane showing the past and wonder why.
	if n := m.scrollOffsetLocked(); n > 0 {
		mark := "↑ " + itoa(n) + " "
		if at := limit - ansi.StringWidth(mark); at > x {
			render.Text(scr, at, area.Min.Y, mark, fgWaiting, bgStrip)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// hitKind is what a point on the strip means.
type hitKind int

const (
	hitNone hitKind = iota
	hitTab
	hitClose
	hitPlus
)

// stripAt reads a pane-local point on the strip. The cross is tested before
// the tab it sits in, or you could never reach it.
func (m *Module) stripAt(x int) (hitKind, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.plusX > 0 && x >= m.plusX && x < m.plusX+ansi.StringWidth(plusLabel) {
		return hitPlus, 0
	}
	for i, t := range m.tabs {
		if t.closeX > 0 && x >= t.closeX && x < t.closeX+ansi.StringWidth(closeLabel) {
			return hitClose, i
		}
		if t.w > 0 && x >= t.x && x < t.x+t.w {
			return hitTab, i
		}
	}
	return hitNone, 0
}

// Mouse switches tabs on the strip, and forwards everything else.
//
// Coordinates arrive pane-local, so the strip is row zero and the module below
// has to be told about the row it does not have.
func (m *Module) Mouse(ev uv.MouseEvent) {
	mouse := ev.Mouse()
	if mouse.Y < stripRows {
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			switch kind, i := m.stripAt(mouse.X); kind {
			case hitPlus:
				if err := m.Add(NewTabModule, nil); err != nil && m.ctx.Status != nil {
					m.ctx.Status(err.Error())
				}
			case hitClose:
				if err := m.CloseTab(i); err != nil && m.ctx.Status != nil {
					m.ctx.Status(err.Error())
				}
			case hitTab:
				m.Select(i)
			}
		}
		return
	}
	if in, ok := m.Active().(module.Inputter); ok {
		in.Mouse(shiftUp(ev, stripRows))
	}
}

// shiftUp moves an event up by n rows, so a module drawn below the strip is
// told where the pointer is in its own terms.
func shiftUp(ev uv.MouseEvent, n int) uv.MouseEvent {
	mouse := ev.Mouse()
	mouse.Y -= n
	switch ev.(type) {
	case uv.MouseClickEvent:
		return uv.MouseClickEvent(mouse)
	case uv.MouseReleaseEvent:
		return uv.MouseReleaseEvent(mouse)
	case uv.MouseWheelEvent:
		return uv.MouseWheelEvent(mouse)
	case uv.MouseMotionEvent:
		return uv.MouseMotionEvent(mouse)
	}
	return ev
}

// Key forwards to the module on screen. Nothing is intercepted: a tab holding
// a Claude session needs every key, and switching tabs is the application's
// business rather than this module's.
func (m *Module) Key(k uv.KeyEvent) {
	if in, ok := m.Active().(module.Inputter); ok {
		in.Key(k)
	}
}

// Paste forwards to the module on screen.
func (m *Module) Paste(text string) {
	if in, ok := m.Active().(module.Inputter); ok {
		in.Paste(text)
	}
}

// Cursor forwards, moved down by the strip.
func (m *Module) Cursor() (x, y int, visible bool) {
	c, ok := m.Active().(module.Cursorer)
	if !ok {
		return 0, 0, false
	}
	cx, cy, visible := c.Cursor()
	if !visible {
		return 0, 0, false
	}
	return cx, cy + stripRows, true
}
