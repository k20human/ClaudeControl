// Package tabs puts several modules in one pane and shows one at a time.
//
// It exists for a window that is not wide enough to split again. Splitting is
// better when there is room — you see two things at once — so this is what you
// reach for when there is not.
//
// Every module in the pane runs, whether or not it is the one on screen: a
// supervisor still supervises from a hidden tab, and a Claude session still
// answers. Only drawing is skipped.
package tabs

import (
	"fmt"
	"image/color"
	"sync"

	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/settings"
)

func init() { module.Register("tabs", New) }

// stripRows is the row the tab strip takes. The module refuses the pane title
// so that this row replaces it rather than adding to it — the point of tabs is
// that space is short.
const stripRows = 1

var (
	bgStrip   = color.RGBA{R: 0x1b, G: 0x21, B: 0x2b, A: 0xff}
	fgIdle    = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	bgActive  = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	fgActive  = color.RGBA{R: 0x10, G: 0x16, B: 0x1e, A: 0xff}
	fgWaiting = color.RGBA{R: 0xe0, G: 0xb0, B: 0x5c, A: 0xff}
)

// tab is one module and what to call it.
type tab struct {
	title string
	mod   module.Module

	// x and w are where its label was last drawn, so a click lands on what
	// was seen rather than on a table that could have drifted from it.
	x, w int

	// waiting marks a session asking for something while you are looking at
	// another tab. Without it, a hidden tab is a session you have forgotten.
	waiting bool
}

// Module is the pane.
type Module struct {
	ctx module.Context

	mu     sync.Mutex
	tabs   []*tab
	active int

	cols, rows int
}

// New builds the module from its configuration.
func New(cfg map[string]any) (module.Module, error) {
	raw, _ := cfg["tabs"].([]any)
	m := &Module{}
	for _, item := range raw {
		spec, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tabs: each tab must be a mapping, got %T", item)
		}
		name, _ := spec["module"].(string)
		if name == "" {
			return nil, fmt.Errorf("tabs: a tab has no %q", "module")
		}
		opts, _ := spec["options"].(map[string]any)
		child, err := module.New(name, opts)
		if err != nil {
			return nil, fmt.Errorf("tabs: %w", err)
		}
		title, _ := spec["title"].(string)
		if title == "" {
			title = name
		}
		m.tabs = append(m.tabs, &tab{title: title, mod: child})
	}
	if len(m.tabs) < 2 {
		// One tab is a pane with a wasted row, and none is a pane with
		// nothing in it. Either is a configuration mistake worth naming.
		return nil, fmt.Errorf("tabs: %d tab configured; a pane of tabs wants at least two", len(m.tabs))
	}
	return m, nil
}

// Init starts every module, not only the one that will be shown first.
func (m *Module) Init(ctx module.Context) error {
	m.ctx = ctx
	for _, t := range m.tabs {
		if err := t.mod.Init(ctx); err != nil {
			return fmt.Errorf("tabs: %s: %w", t.title, err)
		}
	}
	if ctx.Bus == nil {
		return nil
	}
	// A session that wants something while you are on another tab has to be
	// able to say so.
	states := ctx.Bus.SubscribeState(pool.StateTopic)
	go func() {
		for v := range states {
			entries, ok := v.([]*pool.Entry)
			if !ok {
				continue
			}
			if m.noteWaiting(entries) && ctx.Wake != nil {
				ctx.Wake()
			}
		}
	}()
	return nil
}

// noteWaiting marks the tabs whose session is asking for something, and
// reports whether anything changed.
func (m *Module) noteWaiting(entries []*pool.Entry) bool {
	waiting := map[string]bool{}
	for _, e := range entries {
		if e.State == pool.StateWaiting && e.Session != nil {
			waiting[string(e.Session.ID)] = true
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	for i, t := range m.tabs {
		want := false
		if s, ok := t.mod.(interface{ SessionID() string }); ok {
			want = waiting[s.SessionID()]
		}
		// The tab you are looking at is not waiting for you in any useful
		// sense: you are already there.
		if i == m.active {
			want = false
		}
		if t.waiting != want {
			t.waiting, changed = want, true
		}
	}
	return changed
}

// Title refuses the pane title: the strip takes that row instead of adding one.
func (m *Module) Title() (string, bool) { return "", false }

// Resize sizes every module, shown or not, so switching to one never shows it
// laid out for a size it no longer has.
func (m *Module) Resize(w, h int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cols, m.rows = w, h
	inner := h - stripRows
	if inner < 1 {
		inner = 1
	}
	for _, t := range m.tabs {
		if err := t.mod.Resize(w, inner); err != nil {
			return err
		}
	}
	return nil
}

// Close closes every module.
func (m *Module) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	for _, t := range m.tabs {
		if err := t.mod.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Active is the module on screen.
func (m *Module) Active() module.Module {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tabs) == 0 {
		return nil
	}
	return m.tabs[m.active].mod
}

// ActiveTitle is what the tab on screen is called.
func (m *Module) ActiveTitle() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tabs) == 0 {
		return ""
	}
	return m.tabs[m.active].title
}

// Select shows a tab by index.
func (m *Module) Select(i int) {
	m.mu.Lock()
	if i < 0 || i >= len(m.tabs) || i == m.active {
		m.mu.Unlock()
		return
	}
	m.active = i
	// Arriving at a tab answers it, whatever it was asking.
	m.tabs[i].waiting = false
	m.mu.Unlock()
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

// CycleTab moves by delta, wrapping. It is what the application's tab
// shortcuts call.
func (m *Module) CycleTab(delta int) {
	m.mu.Lock()
	n := len(m.tabs)
	if n == 0 {
		m.mu.Unlock()
		return
	}
	next := ((m.active+delta)%n + n) % n
	m.mu.Unlock()
	m.Select(next)
}

// Settings forwards to the module on screen, so the settings menu configures
// what you are looking at.
func (m *Module) Settings() []settings.Setting {
	if p, ok := m.Active().(module.Provider); ok {
		return p.Settings()
	}
	return nil
}
