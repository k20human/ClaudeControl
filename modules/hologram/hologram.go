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
}

// New builds the module. Recognised keys: "style" (sphere, ring or avatar),
// and for the sphere "speed", "trail", "density", "rotation", "breath".
func New(cfg map[string]any) (module.Module, error) {
	style, _ := cfg["style"].(string)
	if style == "" {
		style = "sphere"
	}
	params := paramsFrom(cfg)
	m := &Module{style: style, params: params}
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
	m.renderer.Resize(w, h)
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
	if dt > 0 {
		m.renderer.Step(dt)
	}
	m.renderer.Draw(scr, area)
	m.mu.Unlock()
	m.wake()
}

// Close releases nothing: the module owns no process.
func (m *Module) Close() error { return nil }
