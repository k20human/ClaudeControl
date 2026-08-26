// Package app owns the real terminal and the event loop.
package app

import (
	"fmt"
	"os"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/config"
	"claudecontrol/internal/hooks"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
	"claudecontrol/internal/transcript"

	panel "claudecontrol/modules/settings"
)

// redrawInterval coalesces bursts of guest output into one repaint. Without
// it, a chatty session would trigger thousands of repaints per second.
const redrawInterval = time.Second / 60

// App is the whole application state.
type App struct {
	term *uv.Terminal
	scr  *uv.TerminalScreen

	root    *layout.Node
	modules map[layout.PaneID]module.Module

	bus   *bus.Bus
	pool  *pool.Pool
	hooks *hooks.Listener

	tailerMu sync.Mutex
	tailers  map[string]*transcript.Tailer

	// usage is the last turn seen for each session, kept here so a pane title
	// can show it without subscribing to the bus per pane.
	usageMu sync.RWMutex
	usage   map[string]transcript.Metrics

	// binary is our own executable, which sessions invoke in --hook mode.
	binary string

	focus  layout.PaneID
	prev   layout.PaneID
	zoomed layout.PaneID // 0 when not zoomed

	area  layout.Rect
	rects map[layout.PaneID]layout.Rect
	divs  []layout.DividerRect

	// clearNext forces the next frame to blank the screen first. Set only when
	// the layout changed: see draw for why every frame must not do it.
	clearNext bool

	// hoverDiv and hoverBtn are what the pointer is over, or -1. They exist so
	// a divider can light up under the pointer: a terminal application cannot
	// change the mouse cursor itself, so the affordance has to live on the
	// thing being pointed at.
	hoverDiv int
	hoverBtn int

	overlay       overlayKind
	buttons       []button
	panelButtons  []button
	sessionPanel  selector
	settingsPanel *panel.Panel

	// cfgPath is where settings are written back; moduleNames records what
	// each pane is called there, since the tree itself only holds ids.
	cfgPath     string
	moduleNames map[layout.PaneID]string
	status      string

	palette  *paletteState
	paneDrag *paneDragState

	// layoutChanged records whether the arrangement differs from the file.
	// Saving takes the narrow path while it is false, which is what keeps the
	// comments inside the layout block.
	layoutChanged bool

	// barCountX and barCountW are the slot between the status-bar buttons and
	// quit. The slot is settled during layout so nothing can grow into a
	// button; what it holds is decided when the bar is drawn.
	barCountX int
	barCountW int

	// pointerX and pointerY are the last reported pointer position, so a panel
	// button can light up under it the way a status-bar button does.
	pointerX, pointerY int

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

	b := bus.New()
	a := &App{
		root:        root,
		modules:     make(map[layout.PaneID]module.Module),
		bus:         b,
		pool:        pool.New(b),
		rects:       make(map[layout.PaneID]layout.Rect),
		cfgPath:     cfgPath,
		moduleNames: make(map[layout.PaneID]string),
		hoverDiv:    -1,
		hoverBtn:    -1,
		pointerX:    -1,
		pointerY:    -1,
		wake:        make(chan struct{}, 1),
	}

	// The socket has to exist before any session starts, since a session is
	// told where to send its hooks at launch. A failure here is not fatal: the
	// multiplexer works without state reporting, it simply reports no state.
	a.binary, err = os.Executable()
	if err != nil {
		a.binary = "claudecontrol"
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = os.TempDir()
	}
	if l, lerr := hooks.Listen(runtimeDir); lerr == nil {
		a.hooks = l
		go a.pumpHooks()
	}

	for _, id := range layout.Leaves(root) {
		spec := panes[id]
		m, err := module.New(spec.Module, spec.Options)
		if err != nil {
			return nil, fmt.Errorf("pane %d: %w", id, err)
		}
		if err := m.Init(a.moduleContext(id)); err != nil {
			return nil, fmt.Errorf("pane %d: init: %w", id, err)
		}
		a.modules[id] = m
		a.moduleNames[id] = spec.Module
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

// moduleContext is what every module is handed at Init.
func (a *App) moduleContext(id layout.PaneID) module.Context {
	socket := ""
	if a.hooks != nil {
		socket = a.hooks.Path()
	}
	return module.Context{
		PaneID:     id,
		Pool:       a.pool,
		Bus:        a.bus,
		HookSocket: socket,
		Binary:     a.binary,
		Wake:       a.Wake,
	}
}

// pumpHooks turns hook payloads into session states.
func (a *App) pumpHooks() {
	for p := range a.hooks.Events() {
		// The payload is the only authority on where a transcript lives: the
		// folder name derives from the working directory, two sessions can
		// share one, and a session can be relocated.
		a.followTranscript(p.SessionID, p.TranscriptPath)

		st, ok := pool.StateForHook(p.Event)
		if !ok {
			continue
		}
		a.pool.SetState(session.ID(p.SessionID), st)
		a.Wake()
	}
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
	// The hardware cursor stays hidden for good: the cursor is painted into
	// the cell by drawCursor. Mode 2026 makes each frame atomic, which is
	// what keeps an animated pane from tearing, and it also spares the
	// terminal the hide/show pair ultraviolet otherwise brackets every frame
	// with. Terminals that do not know the mode ignore it.
	a.scr.SetSynchronizedUpdates(true)
	// Full motion tracking. It costs one event per hovered cell, which is the
	// price of highlighting a divider before the pointer is pressed; repaints
	// are coalesced and only a couple of cells ever change.
	a.scr.SetMouseMode(uv.MouseModeMotion)
	a.scr.SetMouseEncoding(uv.MouseEncodingSGR)
	a.scr.EnableBracketedPaste()

	if err := a.term.Start(); err != nil {
		return fmt.Errorf("app: start terminal: %w", err)
	}
	defer func() {
		if a.hooks != nil {
			_ = a.hooks.Close()
		}
		a.stopTranscripts()
		_ = a.pool.CloseAll()
		a.bus.Close()
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

// paneArea is the screen minus the status bar row.
func (a *App) paneArea() layout.Rect {
	r := a.area
	if r.H > 1 {
		r.H--
	}
	return r
}

// relayout recomputes rectangles from the current tree and zoom state.
func (a *App) relayout() {
	old := a.rects
	panes := a.paneArea()
	if a.zoomed != 0 {
		a.rects = map[layout.PaneID]layout.Rect{a.zoomed: panes}
		a.divs = nil
	} else {
		a.rects = layout.Compute(a.root, panes)
		a.divs = layout.Dividers(a.root, panes)
	}
	a.buttons = a.buildStatusBar()
	if a.hoverDiv >= len(a.divs) {
		a.hoverDiv = -1
	}
	a.clearNext = true
	for id, r := range a.rects {
		m, ok := a.modules[id]
		if !ok {
			continue
		}
		// Resize only on a real size change. It is not free: resizing a vt
		// emulator drops its damage marks, which costs the session a full
		// repaint of itself (see session.Resize), and costs the guest a
		// SIGWINCH it may act on.
		if prev, had := old[id]; had && prev.W == r.W && prev.H == r.H {
			continue
		}
		c := contentRect(r)
		_ = m.Resize(c.W, c.H)
	}
	a.saveSnapshot()
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
		c := contentRect(r)
		area := uv.Rect(c.X, c.Y, c.W, c.H)
		m.Draw(a.scr, area)
		if code, dead := a.exitedCode(id); dead {
			exitedBanner(a.scr, area, code)
		}
	}
	a.drawPaneTitles(a.scr)
	drawDividers(a.scr, a.rects, a.divs, a.focus, a.hoverDiv)
	a.drawStatusBar(a.scr)
	a.drawSessionPanel(a.scr)
	a.drawSettingsPanel(a.scr)
	a.drawPaneDrag(a.scr)
	a.drawPalette(a.scr)
	a.drawOverlay(a.scr)
	a.drawCursor(a.scr)
}

// cursorTarget is where the cursor belongs this frame, in screen coordinates,
// and whether it belongs anywhere at all. A panel covers the panes, so while
// one is open the cursor has no business being shown.
func (a *App) cursorTarget() (x, y int, visible bool) {
	if a.overlay != overlayNone || a.sessionPanel != nil || a.settingsPanel != nil || a.palette != nil {
		return 0, 0, false
	}
	m, ok := a.modules[a.focus]
	if !ok {
		return 0, 0, false
	}
	c, ok := m.(module.Cursorer)
	if !ok {
		return 0, 0, false
	}
	cx, cy, visible := c.Cursor()
	if !visible {
		return 0, 0, false
	}
	r := contentRect(a.rects[a.focus])
	return r.X + cx, r.Y + cy, true
}

// drawCursor paints the cursor into the cell instead of asking the terminal
// for its own.
//
// The hardware cursor cannot be used here. Ultraviolet emits the cursor move
// at the head of a frame's byte stream and the cell repainting after it, so
// whatever the application asks for is overwritten by the paint that follows —
// harmless for a still screen, but a pane that animates repaints every frame
// and the cursor was measured parked in the hologram, sixty times a second,
// never once where it was asked to be.
//
// Reversing the cell is under our control, lands exactly where the guest put
// its cursor, and costs nothing. The trade is that it does not blink and is
// always a block, whatever shape the guest asked for. The attribute is
// toggled rather than set so that a cursor sitting on already-reversed text
// still stands out.
func (a *App) drawCursor(scr uv.Screen) {
	x, y, visible := a.cursorTarget()
	if !visible {
		return
	}
	c := scr.CellAt(x, y)
	if c == nil {
		return
	}
	cell := *c
	cell.Style.Attrs ^= uv.AttrReverse
	scr.SetCell(x, y, &cell)
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
