package supervisor

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/render"
	"claudecontrol/internal/session"
)

// headerRows is the button row and the rule under it. The hosted processes are
// sized for what is left, so their output never lands where a header will be
// painted over it.
const headerRows = 2

// The name column follows the longest name, between these bounds; the state
// column is fixed, because it is the one you scan down.
const (
	nameMin = 8
	nameMax = 24
	stateW  = 9
)

var (
	bgPanel = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	bgRule  = color.RGBA{R: 0x24, G: 0x2b, B: 0x38, A: 0xff}
	bgPick  = color.RGBA{R: 0x1e, G: 0x27, B: 0x34, A: 0xff}
	fgText  = color.RGBA{R: 0x9a, G: 0xa8, B: 0xbd, A: 0xff}
	fgMuted = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	fgUp    = color.RGBA{R: 0x6c, G: 0xc4, B: 0x8a, A: 0xff}
	fgDown  = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
	fgHot   = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
)

// Draw paints the list, or the output of the service being looked at.
func (m *Module) Draw(scr uv.Screen, area uv.Rectangle) {
	render.Fill(scr, area, bgPanel)
	m.mu.Lock()
	m.refreshLocked()
	m.hits = m.hits[:0]
	showing := m.showing
	m.mu.Unlock()

	if showing >= 0 {
		m.drawLogs(scr, area, showing)
		return
	}
	m.drawList(scr, area)
}

// hitLocked records a clickable region, pane-local.
//
// Pane-local, because that is how the pointer arrives: the application
// translates a click into the pane's own coordinates before the module ever
// sees it, and a pane of tabs shifts it again past the strip. Everything is
// drawn in screen coordinates, so every region has to be brought back — which
// is why this exists rather than three call sites each remembering to subtract
// the same two numbers.
func (m *Module) hitLocked(area uv.Rectangle, x, y, w int, run func(*Module)) {
	m.hits = append(m.hits, hit{
		x:   x - area.Min.X,
		y:   y - area.Min.Y,
		w:   w,
		run: run,
	})
}

// drawList paints the buttons and one row per service.
func (m *Module) drawList(scr uv.Screen, area uv.Rectangle) {
	m.mu.Lock()
	defer m.mu.Unlock()

	y := area.Min.Y
	x := area.Min.X + 1
	for _, b := range []struct {
		label string
		run   func(*Module)
	}{
		{"▸ start", (*Module).StartPicked},
		{"⟳ restart", (*Module).RestartPicked},
		{"■ stop", (*Module).StopPicked},
		{"⟲ scan", (*Module).Scan},
	} {
		w := ansi.StringWidth(b.label) + 2
		if x+w > area.Max.X {
			break
		}
		render.Text(scr, x, y, " "+b.label+" ", fgHot, bgRule)
		m.hitLocked(area, x, y, w, b.run)
		x += w + 1
	}

	// The count, right-aligned, so a glance answers the only question that
	// matters before any of the detail.
	up := 0
	for _, s := range m.svcs {
		if s.state == Running {
			up++
		}
	}
	count := fmt.Sprintf("%d/%d running", up, len(m.svcs))
	if cx := area.Max.X - 1 - ansi.StringWidth(count); cx > x {
		render.Text(scr, cx, y, count, fgMuted, bgPanel)
	}

	if y+1 < area.Max.Y {
		render.Fill(scr, uv.Rect(area.Min.X, y+1, area.Dx(), 1), bgRule)
	}

	nameW := m.nameWidthLocked(area.Dx())
	for i, s := range m.svcs {
		ry := area.Min.Y + headerRows + i
		if ry >= area.Max.Y {
			return
		}
		m.drawRow(scr, area, ry, i, s, nameW)
	}
}

// nameWidthLocked sizes the name column to the longest name there is, rather
// than to a number chosen in advance. A service is named after the directory
// it runs in, and "provision-relay-api" is not an unusual thing to call one.
func (m *Module) nameWidthLocked(paneW int) int {
	w := nameMin
	for _, s := range m.svcs {
		if n := ansi.StringWidth(s.spec.Name); n > w {
			w = n
		}
	}
	if w > nameMax {
		w = nameMax
	}
	// Never at the expense of the state, which is the column you actually
	// scan.
	if room := paneW - 6 - stateW; w > room {
		w = room
	}
	return w
}

func (m *Module) drawRow(scr uv.Screen, area uv.Rectangle, y, i int, s *service, nameW int) {
	bg := color.Color(bgPanel)
	if i == m.sel {
		bg = bgPick
		render.Fill(scr, uv.Rect(area.Min.X, y, area.Dx(), 1), bgPick)
	}

	box := "[ ]"
	if s.picked {
		box = "[x]"
	}
	x := area.Min.X + 1
	render.Text(scr, x, y, box, fgHot, bg)
	idx := i
	m.hitLocked(area, x, y, 3, func(m *Module) { m.toggle(idx) })
	x += 4

	render.Text(scr, x, y, fmt.Sprintf("%-*s", nameW, clip(s.spec.Name, nameW)), fgText, bg)
	m.hitLocked(area, x, y, nameW, func(m *Module) { m.show(idx) })
	x += nameW + 1

	fg := color.Color(fgMuted)
	detail := ""
	switch s.state {
	case Running:
		fg = fgUp
		switch {
		case s.adopted != nil:
			// No uptime: this process was found, not started, and the moment
			// it began was never measured. A number here would be the moment
			// we noticed, dressed up as something else.
			detail = fmt.Sprintf("pid %-8d adopted · no logs", s.adopted.Pid)
		case s.sess != nil:
			detail = fmt.Sprintf("pid %-8d %s", s.sess.Pid(), forHowLong(s.since))
		}
	case Exited:
		fg = fgDown
		detail = fmt.Sprintf("code %-6d %s ago", s.code, forHowLong(s.since))
	}
	render.Text(scr, x, y, fmt.Sprintf("%-*s", stateW, s.state), fg, bg)
	if x+stateW+1 < area.Max.X {
		render.Text(scr, x+stateW+1, y, clip(detail, area.Max.X-x-stateW-1), fgMuted, bg)
	}
}

// drawLogs paints one service's output, exactly as it wrote it.
func (m *Module) drawLogs(scr uv.Screen, area uv.Rectangle, i int) {
	m.mu.Lock()
	if i < 0 || i >= len(m.svcs) {
		m.showing = -1
		m.mu.Unlock()
		return
	}
	s := m.svcs[i]
	sess := s.sess
	view := &s.view
	carry := s.carry
	adopted := s.adopted
	title := fmt.Sprintf("logs · %s", s.spec.Name)
	state := s.state
	m.hitLocked(area, area.Min.X+1, area.Min.Y, 6, func(m *Module) { m.show(-1) })
	m.mu.Unlock()

	y := area.Min.Y
	render.Text(scr, area.Min.X+1, y, "◀ back", fgHot, bgPanel)
	render.Text(scr, area.Min.X+9, y, clip(title, area.Dx()-10), fgText, bgPanel)
	if y+1 < area.Max.Y {
		render.Fill(scr, uv.Rect(area.Min.X, y+1, area.Dx(), 1), bgRule)
	}

	body := uv.Rect(area.Min.X, area.Min.Y+headerRows, area.Dx(), area.Dy()-headerRows)
	if body.Dy() <= 0 {
		return
	}
	if sess == nil {
		// A stopped service still has a last run, and that is usually what you
		// opened this view to read. It is shown as the plain text it was kept
		// as, under a line saying so — nothing here is live.
		if carry != "" {
			render.Text(scr, body.Min.X+1, body.Min.Y,
				clip("not running · last run below", body.Dx()-2), fgMuted, bgPanel)
			kept := strings.Split(strings.TrimRight(carry, "\n"), "\n")
			room := body.Dy() - 1
			if room < 1 {
				return
			}
			if len(kept) > room {
				kept = kept[len(kept)-room:]
			}
			for i, line := range kept {
				render.Text(scr, body.Min.X+1, body.Min.Y+1+i,
					clip(line, body.Dx()-2), fgMuted, bgPanel)
			}
			return
		}
		lines := []string{"not running — press s, or the start button, to see its output"}
		if state == Exited {
			lines = append(lines, "the output of the run that ended is gone with its process")
		}
		if adopted != nil {
			// Saying why costs two lines and saves the search for a setting
			// that does not exist.
			lines = []string{
				fmt.Sprintf("running as pid %d, started outside this application", adopted.Pid),
				"",
				"its output went wherever it was going before we found it, and",
				"cannot be recovered. restart it — press r — to run it here and",
				"read it from the start.",
			}
		}
		for i, line := range lines {
			if body.Min.Y+i >= body.Max.Y {
				break
			}
			render.Text(scr, body.Min.X+1, body.Min.Y+i, clip(line, body.Dx()-2), fgMuted, bgPanel)
		}
		return
	}
	view.Draw(sess, scr, body)
}

// Cursor puts the cursor where the hosted process put it, so a server that
// asks a question can be answered.
func (m *Module) Cursor() (x, y int, visible bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.showing < 0 || m.showing >= len(m.svcs) {
		return 0, 0, false
	}
	sess := m.svcs[m.showing].sess
	if sess == nil {
		return 0, 0, false
	}
	p := sess.Term.CursorPosition()
	return p.X, p.Y + headerRows, true
}

// Key drives the list, or reaches the process whose output is on screen.
func (m *Module) Key(k uv.KeyEvent) {
	m.mu.Lock()
	target, sel := -1, m.sel
	if m.showing >= 0 && m.showing < len(m.svcs) {
		target = m.showing
	}
	m.mu.Unlock()

	key := k.Key()
	if target >= 0 {
		// Escape is the way out; everything else belongs to the process, which
		// is what makes "press h + enter to show help" work.
		if key.Code == uv.KeyEscape {
			m.show(-1)
			return
		}
		m.mu.Lock()
		s := m.svcs[target].sess
		m.mu.Unlock()
		if s == nil {
			return
		}
		if key.Text != "" {
			s.SendText(key.Text)
			return
		}
		s.SendKey(k)
		return
	}

	switch {
	case key.Code == uv.KeyUp || key.Text == "k":
		m.moveSel(-1)
	case key.Code == uv.KeyDown || key.Text == "j":
		m.moveSel(1)
	case key.Code == uv.KeySpace || key.Text == " ":
		m.toggle(sel)
	case key.Code == uv.KeyEnter:
		m.show(sel)
	case key.Text == "a":
		m.toggleAll()
	case key.Text == "s":
		m.StartPicked()
	case key.Text == "r":
		m.RestartPicked()
	case key.Text == "x":
		m.StopPicked()
	}
}

// Mouse acts on whatever region was clicked. The regions are the ones drawn
// last frame, so what you click is what you saw.
func (m *Module) Mouse(ev uv.MouseEvent) {
	// The wheel belongs to the log while one is open. It is the only way to
	// reach what a restart brought back, which is most of the point of
	// bringing it back.
	if wheel, ok := ev.(uv.MouseWheelEvent); ok {
		m.mu.Lock()
		var sess *session.Session
		var view *session.View
		if m.showing >= 0 && m.showing < len(m.svcs) {
			s := m.svcs[m.showing]
			sess, view = s.sess, &s.view
		}
		m.mu.Unlock()
		if sess == nil || view == nil {
			return
		}
		switch wheel.Button {
		case uv.MouseWheelUp:
			view.Wheel(sess, true)
		case uv.MouseWheelDown:
			view.Wheel(sess, false)
		default:
			return
		}
		if m.ctx.Wake != nil {
			m.ctx.Wake()
		}
		return
	}
	click, ok := ev.(uv.MouseClickEvent)
	if !ok {
		return
	}
	mouse := uv.Mouse(click)

	m.mu.Lock()
	var run func(*Module)
	for _, h := range m.hits {
		if mouse.Y == h.y && mouse.X >= h.x && mouse.X < h.x+h.w {
			run = h.run
			break
		}
	}
	// A click on a row selects it even where nothing is drawn, which is what
	// makes the keyboard shortcuts land where you were looking.
	if run == nil && m.showing < 0 {
		if row := mouse.Y - headerRows; row >= 0 && row < len(m.svcs) {
			m.sel = row
		}
	}
	m.mu.Unlock()

	if run != nil {
		run(m)
	}
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

// Paste is ignored in the list and handed to the process otherwise.
func (m *Module) Paste(text string) {
	m.mu.Lock()
	var s *service
	if i := m.showing; i >= 0 && i < len(m.svcs) {
		s = m.svcs[i]
	}
	m.mu.Unlock()
	if s != nil && s.sess != nil {
		s.sess.Paste(text)
	}
}

func (m *Module) moveSel(d int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sel += d
	if m.sel < 0 {
		m.sel = 0
	}
	if m.sel >= len(m.svcs) {
		m.sel = len(m.svcs) - 1
	}
}

func (m *Module) toggle(i int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i < 0 || i >= len(m.svcs) {
		return
	}
	m.svcs[i].picked = !m.svcs[i].picked
	m.sel = i
}

// toggleAll picks everything, or nothing if everything was already picked.
func (m *Module) toggleAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := true
	for _, s := range m.svcs {
		if !s.picked {
			all = false
			break
		}
	}
	for _, s := range m.svcs {
		s.picked = !all
	}
}

// show opens a service's output, or returns to the list with -1.
func (m *Module) show(i int) {
	m.mu.Lock()
	if i >= len(m.svcs) {
		i = -1
	}
	m.showing = i
	if i >= 0 {
		m.sel = i
	}
	m.mu.Unlock()
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

// Showing is the index of the service whose output fills the pane, or -1.
func (m *Module) Showing() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.showing
}

// forHowLong is a duration a person reads at a glance, not to the second past
// an hour.
func forHowLong(since time.Time) string {
	if since.IsZero() {
		return ""
	}
	d := time.Since(since)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", max(int(d.Seconds()), 0))
}

func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}
