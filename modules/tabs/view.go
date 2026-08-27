package tabs

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/module"
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

	x, hidden := area.Min.X, 0
	for i, t := range m.tabs {
		label := " " + t.title + " "
		if t.waiting {
			label = " " + t.title + " • "
		}
		w := ansi.StringWidth(label)
		if x+w > area.Max.X {
			t.x, t.w = 0, 0
			hidden++
			continue
		}
		fg, bg := color.Color(fgIdle), color.Color(bgStrip)
		switch {
		case i == m.active:
			fg, bg = fgActive, bgActive
		case t.waiting:
			fg = fgWaiting
		}
		render.Fill(scr, uv.Rect(x, area.Min.Y, w, 1), bg)
		render.Text(scr, x, area.Min.Y, label, fg, bg)
		// Recorded pane-local, because that is how the pointer arrives: the
		// application translates a click into the pane's own coordinates
		// before the module ever sees it.
		t.x, t.w = x-area.Min.X, w
		x += w
	}

	if hidden > 0 {
		mark := "+" + itoa(hidden)
		if at := area.Max.X - ansi.StringWidth(mark); at > x {
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

// tabAt is the tab whose label covers a pane-local point on the strip, or -1.
func (m *Module) tabAt(x int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.tabs {
		if t.w > 0 && x >= t.x && x < t.x+t.w {
			return i
		}
	}
	return -1
}

// Mouse switches tabs on the strip, and forwards everything else.
//
// Coordinates arrive pane-local, so the strip is row zero and the module below
// has to be told about the row it does not have.
func (m *Module) Mouse(ev uv.MouseEvent) {
	mouse := ev.Mouse()
	if mouse.Y < stripRows {
		if _, isClick := ev.(uv.MouseClickEvent); isClick {
			if i := m.tabAt(mouse.X); i >= 0 {
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
