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
	"claudecontrol/internal/indicator"
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

	// savedSessions is what the snapshot on disk holds, so it is rewritten
	// only when the conversations actually change.
	savedSessions []string

	// usage is the last turn seen for each session, kept here so a pane title
	// can show it without subscribing to the bus per pane.
	usageMu sync.RWMutex
	usage   map[string]transcript.Metrics
	names   map[string]string

	// live maps a pane's own session id to the one Claude Code is using for
	// it now. They differ from the moment a conversation is resumed.
	live map[string]string

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

	// status is a sentence for the bar, and statusAt when it was said. It is
	// how the application answers something it was asked to do but could not.
	status   string
	statusAt time.Time

	palette  *paletteState
	paneDrag *paneDragState

	// menu is the open context menu, if any. It exists because enabling mouse
	// reporting takes the right button away from the terminal, and with it the
	// menu the terminal would otherwise have shown.
	menu *menuState

	// conv is the open search across the conversations on disk.
	conv *convSearch

	// find is the open search, if any. It names the pane it searches: what
	// you are looking for is in that pane's history.
	find *findState

	// clipboardAsked is when the terminal was last asked for the clipboard
	// over OSC 52, so its silence can be reported rather than waited on.
	clipboardAsked time.Time

	// layoutChanged records whether the arrangement differs from the file.
	// Saving takes the narrow path while it is false, which is what keeps the
	// comments inside the layout block.
	layoutChanged bool

	// barCountX and barCountW are the slot between the status-bar buttons and
	// quit. The slot is settled during layout so nothing can grow into a
	// button; what it holds is decided when the bar is drawn.
	barCountX int
	barCountW int

	// barUsageX and barUsageW are the slot for the account budget, reserved
	// only while some pane is fetching one.
	barUsageX int
	barUsageW int

	// barUsageForm is which of usageForms the reserved slot can hold.
	barUsageForm int

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

	// The conversations the last run left behind, handed out in the order the
	// panes appear — the same order they were recorded in.
	resuming, _ := LoadSnapshot(SnapshotPath())
	pending := resumable(resuming.Sessions)

	for _, id := range layout.Leaves(root) {
		spec := panes[id]
		m, err := module.New(spec.Module, withResume(spec.Module, spec.Options, &pending))
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

// resumable drops the sessions that cannot be resumed.
//
// A session that was opened and never spoken to has no transcript, and asking
// Claude Code to resume it fails with "no conversation found with session ID"
// — an error on the pane, at startup, about something the person did not do
// and can do nothing about. Checking first turns that into a fresh pane, which
// is what they wanted anyway.
func resumable(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if transcript.Exists(id) {
			out = append(out, id)
		}
	}
	return out
}

// withResume gives a pane about to be built the session it had last time.
//
// In the order they appear, which is the order they were saved in. Rearranging
// the panes between runs therefore hands a conversation to a different pane —
// predictable, and far less surprising than losing it. A resume written in the
// configuration by hand always wins: it was put there on purpose.
func withResume(name string, opts map[string]any, pending *[]string) map[string]any {
	switch name {
	case "claude":
		if len(*pending) == 0 {
			return opts
		}
		if _, set := opts["resume"]; set {
			return opts
		}
		id := (*pending)[0]
		*pending = (*pending)[1:]
		out := make(map[string]any, len(opts)+1)
		for k, v := range opts {
			out[k] = v
		}
		out["resume"] = id
		return out

	case "tabs":
		raw, ok := opts["tabs"].([]any)
		if !ok {
			return opts
		}
		tabs := make([]any, 0, len(raw))
		for _, item := range raw {
			spec, ok := item.(map[string]any)
			if !ok {
				tabs = append(tabs, item)
				continue
			}
			inner, _ := spec["module"].(string)
			nested, _ := spec["options"].(map[string]any)
			copied := make(map[string]any, len(spec))
			for k, v := range spec {
				copied[k] = v
			}
			copied["options"] = withResume(inner, nested, pending)
			tabs = append(tabs, copied)
		}
		out := make(map[string]any, len(opts))
		for k, v := range opts {
			out[k] = v
		}
		out["tabs"] = tabs
		return out
	}
	return opts
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
		Status:     func(msg string) { a.setStatus("%s", msg) },
		Wake:       a.Wake,
	}
}

// pumpHooks turns hook payloads into session states.
func (a *App) pumpHooks() {
	for p := range a.hooks.Events() {
		// Keyed by the pane rather than by the payload's session id, because
		// the two part company: resuming a conversation makes Claude Code
		// write a new transcript under a new id, and everything keyed on the
		// old one — the state mark, the token counts, the conversation to
		// bring back — quietly stops describing anything.
		//
		// The pane's own id is on the hook's command line and never moves.
		// Older hooks, registered before a session was restarted, carry none;
		// falling back on the payload keeps them working.
		key := p.Pane
		if key == "" {
			key = p.SessionID
		}
		if p.Pane != "" && p.SessionID != "" {
			a.noteLiveSession(p.Pane, p.SessionID)
		}

		// The payload is the only authority on where a transcript lives: the
		// folder name derives from the working directory, two sessions can
		// share one, and a session can be relocated.
		a.followTranscript(key, p.TranscriptPath)

		st, ok := pool.StateForHook(p.Event)
		if !ok {
			continue
		}
		a.pool.SetState(session.ID(key), st)
		a.Wake()
	}
}

// noteLiveSession records what Claude Code is currently calling a pane's
// conversation, which is what a later run has to resume.
func (a *App) noteLiveSession(pane, live string) {
	a.usageMu.Lock()
	if a.live == nil {
		a.live = make(map[string]string)
	}
	changed := a.live[pane] != live
	a.live[pane] = live
	a.usageMu.Unlock()
	if changed {
		a.Wake()
	}
}

// liveSession is the conversation actually running behind a pane, which is the
// one we started until Claude Code says otherwise.
func (a *App) liveSession(pane string) string {
	a.usageMu.RLock()
	defer a.usageMu.RUnlock()
	if live, ok := a.live[pane]; ok && live != "" {
		return live
	}
	return pane
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
		// Modules first: one that owns processes of its own has to be given
		// the chance to end them, and only it knows how. Closing the pool
		// first would leave them running with nothing left to ask.
		a.closeModules()
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

	// A countdown is computed when it is drawn, and nothing else on a still
	// screen asks for a redraw. Without this the "refills in" figure would sit
	// unchanged until the next reading — up to three minutes wrong, which is
	// worse than a coarser unit honestly kept.
	countdown := time.NewTicker(time.Minute)
	defer countdown.Stop()

	// The working mark turns, and a still screen asks for no redraw of its
	// own. This ticks only while something is actually working, so an idle
	// interface stays idle.
	spin := time.NewTicker(indicator.Period)
	defer spin.Stop()

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
		case <-spin.C:
			if a.anySessionWorking() {
				dirty = true
			}
		case <-countdown.C:
			// Armed, never cleared: whatever else asked for a redraw still
			// gets one.
			if a.hasAccountSource() {
				dirty = true
			}
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
		c := shrinkTop(r, a.paneTitleH(id))
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
	a.clipboardTimedOut()
	if a.clearNext {
		a.clearNext = false
		for y := 0; y < a.area.H; y++ {
			for x := 0; x < a.area.W; x++ {
				a.scr.SetCell(x, y, nil)
			}
		}
	}
	for id := range a.rects {
		m, ok := a.modules[id]
		if !ok {
			continue
		}
		c := a.contentRect(id)
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
	a.drawFind(a.scr)
	a.drawConvSearch(a.scr)
	a.drawMenu(a.scr)
	a.drawPalette(a.scr)
	a.drawOverlay(a.scr)
	a.drawCursor(a.scr)
	a.setWindowTitle()
	a.saveSnapshot()
}

// cursorTarget is where the cursor belongs this frame, in screen coordinates,
// and whether it belongs anywhere at all. A panel covers the panes, so while
// one is open the cursor has no business being shown.
func (a *App) cursorTarget() (x, y int, visible bool) {
	if a.overlay != overlayNone || a.sessionPanel != nil || a.settingsPanel != nil ||
		a.palette != nil || a.menu != nil || a.find != nil || a.conv != nil {
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
	r := a.contentRect(a.focus)
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

// windowTitle summarises every session for the terminal running this
// application.
//
// The terminal's own tab is the one place you can see when the window is
// behind something else, which is exactly when a session asking a question
// would otherwise go unnoticed. Claude Code writes such a title for itself;
// hosting it takes that away, so the summary is written here instead.
func (a *App) windowTitle() string {
	if a.pool == nil {
		return "ClaudeControl"
	}
	entries := a.pool.All()
	states := make([]pool.State, 0, len(entries))
	counts := map[pool.State]int{}
	for _, e := range entries {
		states = append(states, e.State)
		counts[e.State]++
	}
	worst := indicator.Worst(states)
	if worst == pool.StateIdle {
		return "ClaudeControl"
	}
	return fmt.Sprintf("%s %d %s — ClaudeControl",
		indicator.Glyph(worst, time.Now()), counts[worst], worst)
}

// setWindowTitle writes it, and only when it changed: a title rewritten every
// frame is a title the terminal repaints every frame.
func (a *App) setWindowTitle() {
	title := a.windowTitle()
	if a.scr.WindowTitle() == title {
		return
	}
	a.scr.SetWindowTitle(title)
}

// anySessionWorking reports whether something is turning, which is what
// decides whether the interface has to keep redrawing for the animation.
func (a *App) anySessionWorking() bool {
	if a.pool == nil {
		return false
	}
	for _, e := range a.pool.All() {
		if indicator.Animated(e.State) {
			return true
		}
	}
	return false
}

// closeModules releases every module. Panes are closed one at a time as they
// are removed; this is the other end, when the application itself goes.
func (a *App) closeModules() {
	for id, m := range a.modules {
		_ = m.Close()
		delete(a.modules, id)
	}
}

// handle dispatches one terminal event.
func (a *App) handle(ev uv.Event) {
	switch e := ev.(type) {
	case uv.WindowSizeEvent:
		a.resize(e.Width, e.Height)
	case uv.KeyPressEvent:
		a.handleKey(e)
	case uv.ClipboardEvent:
		// The terminal answered, so it does hand the clipboard over after all.
		a.clipboardAsked = time.Time{}
		a.status = ""
		a.deliverPaste(e.Content)

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
