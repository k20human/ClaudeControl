// Package sessions lists every session the pool holds, attached or not. It is
// the one place that answers "is Claude waiting for me anywhere?".
package sessions

import (
	"image/color"
	"strings"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/render"
	"claudecontrol/internal/session"
)

func init() { module.Register("sessions", New) }

var (
	fgNormal   = color.RGBA{R: 0xc7, G: 0xd2, B: 0xe0, A: 0xff}
	fgDim      = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	fgWaiting  = color.RGBA{R: 0xe5, G: 0x93, B: 0x3a, A: 0xff}
	fgExited   = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
	bgSelected = color.RGBA{R: 0x24, G: 0x2e, B: 0x3d, A: 0xff}
	bgPanel    = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
)

// Module renders the session list.
//
// The entries arrive from the bus, on a goroutine of their own, while the
// application draws from another — hence the mutex around them.
type Module struct {
	ctx module.Context

	mu       sync.RWMutex
	entries  []*pool.Entry
	selected int
}

// New builds the module. It takes no options.
func New(map[string]any) (module.Module, error) { return &Module{}, nil }

// Init subscribes to the pool's announcements.
func (m *Module) Init(ctx module.Context) error {
	m.ctx = ctx
	if ctx.Pool != nil {
		m.SetEntries(ctx.Pool.All())
	}
	if ctx.Bus != nil {
		ch := ctx.Bus.SubscribeState(pool.StateTopic)
		go func() {
			for v := range ch {
				if entries, ok := v.([]*pool.Entry); ok {
					m.SetEntries(entries)
					if ctx.Wake != nil {
						ctx.Wake()
					}
				}
			}
		}()
	}
	return nil
}

// SetEntries replaces the list, keeping the selection on the same session when
// it is still there.
func (m *Module) SetEntries(entries []*pool.Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var wanted session.ID
	if m.selected >= 0 && m.selected < len(m.entries) {
		if e := m.entries[m.selected]; e.Session != nil {
			wanted = e.Session.ID
		}
	}
	m.entries = entries
	m.selected = 0
	for i, e := range entries {
		if e.Session != nil && e.Session.ID == wanted {
			m.selected = i
			break
		}
	}
}

// Selected returns the highlighted session.
func (m *Module) Selected() (session.ID, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.selected < 0 || m.selected >= len(m.entries) {
		return "", false
	}
	e := m.entries[m.selected]
	if e.Session == nil {
		return "", false
	}
	return e.Session.ID, true
}

// MoveSelection moves the highlight, stopping at either end.
func (m *Module) MoveSelection(delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selected += delta
	if m.selected >= len(m.entries) {
		m.selected = len(m.entries) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// Entries is the current list.
func (m *Module) Entries() []*pool.Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.entries
}

// Resize records nothing: the list draws to whatever area it is given.
func (m *Module) Resize(int, int) error { return nil }

// Row renders one entry, clipped to width.
//
// The waiting state is spelled out rather than coded into a symbol: it is the
// single fact the whole module exists to convey, and it should not need a
// legend.
func Row(e *pool.Entry, width int) string {
	state := e.State.String()
	if e.State == pool.StateWaiting {
		state = "WAITING FOR YOU"
	}
	parts := []string{e.Title}
	if !e.Attached {
		parts = append(parts, "detached")
	}
	parts = append(parts, state)
	line := strings.Join(parts, "  ")
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	return line
}

// Draw paints the list.
func (m *Module) Draw(scr uv.Screen, area uv.Rectangle) {
	m.mu.RLock()
	entries := m.entries
	selected := m.selected
	m.mu.RUnlock()

	render.Fill(scr, area, bgPanel)
	if len(entries) == 0 {
		render.Text(scr, area.Min.X, area.Min.Y, "no sessions", fgDim, bgPanel)
		return
	}
	for i, e := range entries {
		y := area.Min.Y + i
		if y >= area.Max.Y {
			return
		}
		fg := color.Color(fgNormal)
		switch e.State {
		case pool.StateWaiting:
			fg = fgWaiting
		case pool.StateExited:
			fg = fgExited
		}
		if !e.Attached {
			fg = fgDim
		}
		bg := color.Color(bgPanel)
		if i == selected {
			bg = bgSelected
		}
		render.Fill(scr, uv.Rect(area.Min.X, y, area.Dx(), 1), bg)
		render.Text(scr, area.Min.X, y, Row(e, area.Dx()), fg, bg)
	}
}

// Close does nothing: the module owns no session.
func (m *Module) Close() error { return nil }
