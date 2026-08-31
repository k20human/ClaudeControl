// Package hologram draws the animated panel. Three renderers share one module:
// the particle sphere, a ring gauge and a state avatar.
package hologram

import (
	"fmt"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/holo"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/transcript"
)

func init() { module.Register("hologram", New) }

// Signal is what the hologram is told about the sessions. Every renderer takes
// the same one, so switching style never changes what is being shown.
type Signal struct {
	Active   int    // sessions currently working
	Worst    string // worst state across the pool: idle, working, waiting, exited
	Context  int    // context tokens in use, absolute
	Thinking int    // thinking tokens of the last turn
}

// Renderer is one way of drawing the signal.
type Renderer interface {
	Resize(cols, rows int)
	Step(dt float64)
	Draw(scr uv.Screen, area uv.Rectangle)
	SetSignal(s Signal)
}

// Module is the pane.
type Module struct {
	ctx   module.Context
	style string

	// mu guards the renderer and the signal. Both are reached from the
	// subscriptions as well as from the draw loop, and a renderer is a pile of
	// floating-point state that must not be written mid-frame.
	mu       sync.Mutex
	renderer Renderer
	params   holo.Params
	sig      Signal

	cols int
	rows int
	last time.Time

	// side is where the text column goes: "off", "left" or "right".
	side    string
	readout *readout
	// prev is each session's last known state, so a transition can be told
	// from a repeat of the same announcement.
	prev map[string]poolState

	// pace is how often the panel asks for the next frame. It is what sets
	// the rate the whole application redraws at while this panel is on
	// screen, which is why it is not the draw loop's business but this one's.
	pace *pacer
}

// poolState is aliased so the map above reads without the package name.
type poolState = pool.State

// New builds the module. Recognised keys: "style" (sphere, ring or avatar),
// "fps" (how often it asks to be redrawn, default 20),
// and for the sphere "speed", "trail", "density", "rotation", "breath".
func New(cfg map[string]any) (module.Module, error) {
	style, _ := cfg["style"].(string)
	if style == "" {
		style = "sphere"
	}
	side, ok := cfg["readout"].(string)
	if !ok {
		side = "right"
	}
	switch side {
	case "off", "left", "right", "overlay":
	default:
		return nil, fmt.Errorf(
			"hologram: unknown readout %q (want off, left, right or overlay)", side)
	}

	fps := float64(DefaultFPS)
	if v, ok := asFloat(cfg["fps"]); ok {
		if v <= 0 || v > maxFPS {
			return nil, fmt.Errorf("hologram: fps %v is outside 1..%d", v, maxFPS)
		}
		fps = v
	}

	params := paramsFrom(cfg)
	m := &Module{
		style:   style,
		params:  params,
		side:    side,
		pace:    newPacer(fps),
		readout: newReadout(params.Seed),
		prev:    map[string]poolState{},
	}
	switch style {
	case "sphere":
		m.renderer = newSphere(params)
	case "ring":
		m.renderer = newRing()
	case "avatar":
		m.renderer = newAvatar()
	default:
		return nil, fmt.Errorf("hologram: unknown style %q (want sphere, ring or avatar)", style)
	}
	return m, nil
}

// paramsFrom overlays configured values on the ones validated by eye.
func paramsFrom(cfg map[string]any) holo.Params {
	p := holo.DefaultParams()
	set := func(key string, dst *float64) {
		switch v := cfg[key].(type) {
		case float64:
			*dst = v
		case int:
			*dst = float64(v)
		}
	}
	set("speed", &p.Speed)
	set("trail", &p.Trail)
	set("density", &p.Density)
	set("rotation", &p.Rotation)
	set("breath", &p.Breath)
	return p
}

// Init subscribes to the pool so the animation follows what sessions are doing.
func (m *Module) Init(ctx module.Context) error {
	m.ctx = ctx
	m.last = time.Now()
	if ctx.Bus == nil {
		return nil
	}
	states := ctx.Bus.SubscribeState(pool.StateTopic)
	go func() {
		for v := range states {
			entries, ok := v.([]*pool.Entry)
			if !ok {
				continue
			}
			m.noteTransitions(entries)
			m.mu.Lock()
			sig := signalFrom(entries)
			// The token counts belong to the turns, not to the pool: keep
			// whatever the last turn reported rather than blanking it every
			// time a session changes state.
			sig.Context, sig.Thinking = m.sig.Context, m.sig.Thinking
			m.sig = sig
			m.renderer.SetSignal(sig)
			m.mu.Unlock()
			m.wake()
		}
	}()

	// Turns are what a spark marks. Nothing emits one on a timer, so the panel
	// is quiet exactly when the sessions are.
	turns := ctx.Bus.SubscribeState(transcript.SessionTopic)
	go func() {
		for v := range turns {
			sm, ok := v.(transcript.SessionMetrics)
			if !ok {
				continue
			}
			m.readout.Note(time.Now(), turnLine(sm.Metrics))
			m.mu.Lock()
			m.sig.Context, m.sig.Thinking = sm.Metrics.Context, sm.Metrics.Thinking
			m.renderer.SetSignal(m.sig)
			if e, ok := m.renderer.(interface{ Emit(int) }); ok {
				e.Emit(sparksFor(sm.Metrics))
			}
			m.mu.Unlock()
			m.wake()
		}
	}()
	return nil
}

// noteTransitions writes the pool's changes into the log. Only changes: the
// pool announces its whole state on every event, and repeating "thinking" once
// a second would bury everything that actually happened.
func (m *Module) noteTransitions(entries []*pool.Entry) {
	named := len(entries) > 1
	now := time.Now()
	seen := make(map[string]bool, len(entries))

	for _, e := range entries {
		if e.Session == nil {
			continue
		}
		id := string(e.Session.ID)
		seen[id] = true
		was, known := m.prev[id]
		m.prev[id] = e.State
		if !known {
			m.readout.Note(now, withName(e.Title, "session started", named))
			continue
		}
		if was == e.State {
			continue
		}
		if text := transitionText(was, e.State); text != "" {
			m.readout.Note(now, withName(e.Title, text, named))
		}
	}
	for id := range m.prev {
		if !seen[id] {
			delete(m.prev, id)
		}
	}
}

// transitionText names a change worth reporting, and returns nothing for one
// that is not.
func transitionText(was, now pool.State) string {
	switch now {
	case pool.StateWorking:
		return "thinking…"
	case pool.StateWaiting:
		return "waiting for you"
	case pool.StateExited:
		return "session ended"
	case pool.StateIdle:
		if was == pool.StateWorking {
			return "done"
		}
	}
	return ""
}

// withName prefixes an event with its session, but only while more than one is
// running: with a single session the name is on every line and says nothing.
func withName(title, text string, named bool) string {
	if !named || title == "" {
		return text
	}
	return title + "  " + text
}

// sparksFor is how many traces a turn is worth. A turn always shows at least
// one, and a turn that thought hard shows more — but the count is capped well
// under what the sphere can hold, so a long reasoning burst reads as busy
// rather than as a flash.
func sparksFor(met transcript.Metrics) int {
	n := 1 + met.Thinking/400
	if n > 6 {
		n = 6
	}
	return n
}

func (m *Module) wake() {
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

// signalFrom reduces the pool to what the hologram reacts to.
func signalFrom(entries []*pool.Entry) Signal {
	s := Signal{Worst: pool.StateIdle.String()}
	worst := pool.StateIdle
	for _, e := range entries {
		if e.State == pool.StateWorking {
			s.Active++
		}
		// Ordered by how much it wants your attention, not by the constant's
		// value: exited beats waiting beats working.
		if rank(e.State) > rank(worst) {
			worst = e.State
		}
	}
	s.Worst = worst.String()
	return s
}

func rank(s pool.State) int {
	switch s {
	case pool.StateExited:
		return 3
	case pool.StateWaiting:
		return 2
	case pool.StateWorking:
		return 1
	default:
		return 0
	}
}

// Title reports that the hologram wants no title row. It is a picture; a
// label above it would say what you can already see and cost it a row.
func (m *Module) Title() (string, bool) { return "", false }

// Resize passes the new size on.
func (m *Module) Resize(w, h int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cols, m.rows = w, h
	m.renderer.Resize(w-columnW(w, m.side), h)
	return nil
}

// Step advances the animation. Exposed so tests can drive it deterministically
// rather than sleeping.
func (m *Module) Step(dt float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renderer.Step(dt)
}

// Draw advances by however long has passed and paints.
func (m *Module) Draw(scr uv.Screen, area uv.Rectangle) {
	now := time.Now()
	dt := now.Sub(m.last).Seconds()
	m.last = now
	// A pane that was hidden, or a machine that slept, must not fast-forward
	// the whole gap in one frame.
	if dt > 0.2 {
		dt = 0.2
	}
	m.mu.Lock()
	state := m.sig.Worst
	m.mu.Unlock()
	m.readout.Observe(state, now)

	sphere, column, split := readoutSplit(area, m.side)

	m.mu.Lock()
	if dt > 0 {
		m.renderer.Step(dt)
	}
	m.renderer.Draw(scr, sphere)
	m.mu.Unlock()

	switch {
	case split:
		m.readout.draw(scr, column, now)
	case m.side == "overlay":
		m.readout.drawOver(scr, area, now)
	}
	// Across the whole pane rather than inside the column: the corner is the
	// corner, column or no column.
	m.readout.drawMaxim(scr, area)
	// The next frame, at this panel's own pace rather than as fast as the
	// application will go.
	m.pace.ask(m.wake)
}

// Close stops asking for frames. The module owns no process, but a panel that
// went on waking a closed application would be a leak of the same kind.
func (m *Module) Close() error {
	m.pace.stop()
	return nil
}
