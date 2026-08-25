// Package app owns the real terminal and the event loop.
package app

import (
	"fmt"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/config"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/session"
)

// redrawInterval coalesces bursts of guest output into one repaint. Without
// it, a chatty session would trigger thousands of repaints per second.
const redrawInterval = time.Second / 60

// App is the whole application state.
type App struct {
	term *uv.Terminal
	scr  *uv.TerminalScreen

	root     *layout.Node
	modules  map[layout.PaneID]module.Module
	sessions *session.Registry

	focus  layout.PaneID
	prev   layout.PaneID
	zoomed layout.PaneID // 0 when not zoomed

	area  layout.Rect
	rects map[layout.PaneID]layout.Rect
	divs  []layout.DividerRect

	// clearNext forces the next frame to blank the screen first. Set only when
	// the layout changed: see draw for why every frame must not do it.
	clearNext bool

	nextPane layout.PaneID
	drag     *dragState

	wake chan struct{}
	quit bool
}

// New builds the application from a configuration file.
func New(cfgPath string) (*App, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	root, panes, err := config.Build(cfg)
	if err != nil {
		return nil, err
	}

	a := &App{
		root:     root,
		modules:  make(map[layout.PaneID]module.Module),
		sessions: session.NewRegistry(),
		rects:    make(map[layout.PaneID]layout.Rect),
		wake:     make(chan struct{}, 1),
	}

	for _, id := range layout.Leaves(root) {
		spec := panes[id]
		m, err := module.New(spec.Module, spec.Options)
		if err != nil {
			return nil, fmt.Errorf("pane %d: %w", id, err)
		}
		if err := m.Init(module.Context{
			PaneID: id, Sessions: a.sessions, Wake: a.Wake,
		}); err != nil {
			return nil, fmt.Errorf("pane %d: init: %w", id, err)
		}
		a.modules[id] = m
		if id > a.nextPane {
			a.nextPane = id
		}
	}
	if ids := layout.Leaves(root); len(ids) > 0 {
		a.focus = ids[0]
		a.prev = ids[0]
	}
	return a, nil
}

// Wake asks for a repaint. It never blocks: a full channel already means a
// repaint is pending.
func (a *App) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Run takes over the terminal and loops until the user quits.
func (a *App) Run() error {
	a.term = uv.DefaultTerminal()
	a.scr = a.term.Screen()

	a.scr.EnterAltScreen()
	a.scr.HideCursor()
	// Drag tracking, not motion tracking: one event per hovered cell would
	// force a repaint for no benefit at this stage.
	a.scr.SetMouseMode(uv.MouseModeDrag)
	a.scr.SetMouseEncoding(uv.MouseEncodingSGR)
	a.scr.EnableBracketedPaste()

	if err := a.term.Start(); err != nil {
		return fmt.Errorf("app: start terminal: %w", err)
	}
	defer func() {
		_ = a.sessions.CloseAll()
		a.scr.ExitAltScreen()
		a.scr.ShowCursor()
		a.scr.SetMouseMode(uv.MouseModeNone)
		a.scr.DisableBracketedPaste()
		_ = a.scr.Flush()
		_ = a.term.Stop()
	}()

	w, h, err := a.term.GetSize()
	if err != nil {
		return fmt.Errorf("app: terminal size: %w", err)
	}
	a.resize(w, h)

	ticker := time.NewTicker(redrawInterval)
	defer ticker.Stop()

	dirty := true
	for !a.quit {
		select {
		case ev, ok := <-a.term.Events():
			if !ok {
				return nil
			}
			a.handle(ev)
			dirty = true
		case <-a.wake:
			dirty = true
		case <-ticker.C:
			if !dirty {
				continue
			}
			dirty = false
			a.draw()
			a.scr.Render()
			if err := a.scr.Flush(); err != nil {
				return fmt.Errorf("app: flush: %w", err)
			}
		}
	}
	return nil
}

// resize recomputes every rectangle and tells each module its new size.
func (a *App) resize(w, h int) {
	a.scr.Resize(w, h)
	a.area = layout.Rect{X: 0, Y: 0, W: w, H: h}
	a.relayout()
}

// relayout recomputes rectangles from the current tree and zoom state.
func (a *App) relayout() {
	old := a.rects
	if a.zoomed != 0 {
		a.rects = map[layout.PaneID]layout.Rect{a.zoomed: a.area}
		a.divs = nil
	} else {
		a.rects = layout.Compute(a.root, a.area)
		a.divs = layout.Dividers(a.root, a.area)
	}
	a.clearNext = true
	for id, r := range a.rects {
		m, ok := a.modules[id]
		if !ok {
			continue
		}
		// Resize only on a real size change. Resizing a vt emulator drops its
		// damage marks, and Draw copies nothing for a row it does not consider
		// damaged — so a redundant resize blanks a pane until its guest writes
		// again. A genuine resize has the same effect, but there the guest gets
		// a SIGWINCH and repaints itself.
		if prev, had := old[id]; had && prev.W == r.W && prev.H == r.H {
			continue
		}
		_ = m.Resize(r.W, r.H)
	}
}

// draw paints every visible pane, then the chrome, then places the cursor.
//
// The screen is treated as persistent. vt.Terminal.Draw is an incremental
// update, not a full repaint: it copies only the rows its emulator still
// considers damaged, and an emulator whose damage list is empty copies nothing
// at all. Blanking the screen on every frame would therefore erase panes that
// have simply not written anything since the last frame.
func (a *App) draw() {
	if a.clearNext {
		a.clearNext = false
		for y := 0; y < a.area.H; y++ {
			for x := 0; x < a.area.W; x++ {
				a.scr.SetCell(x, y, nil)
			}
		}
	}
	for id, r := range a.rects {
		m, ok := a.modules[id]
		if !ok {
			continue
		}
		m.Draw(a.scr, uv.Rect(r.X, r.Y, r.W, r.H))
	}
	drawChrome(a.scr, a.rects, a.divs, a.focus)

	a.scr.HideCursor()
	if m, ok := a.modules[a.focus]; ok {
		if c, ok := m.(module.Cursorer); ok {
			if x, y, visible := c.Cursor(); visible {
				r := a.rects[a.focus]
				a.scr.SetCursorPosition(r.X+x, r.Y+y)
				a.scr.ShowCursor()
			}
		}
	}
}

// handle dispatches one terminal event.
func (a *App) handle(ev uv.Event) {
	switch e := ev.(type) {
	case uv.WindowSizeEvent:
		a.resize(e.Width, e.Height)
	case uv.KeyPressEvent:
		a.handleKey(e)
	case uv.PasteEvent:
		// Pasted text is forwarded whole and never scanned for bindings.
		if m, ok := a.modules[a.focus].(module.Inputter); ok {
			m.Paste(e.Content)
		}
	case uv.MouseClickEvent:
		a.handleMouse(e, e.Mouse())
	case uv.MouseReleaseEvent:
		a.handleMouse(e, e.Mouse())
	case uv.MouseWheelEvent:
		a.handleMouse(e, e.Mouse())
	case uv.MouseMotionEvent:
		a.handleMouse(e, e.Mouse())
	}
}
