package hologram

import (
	"fmt"

	"claudecontrol/internal/holo"
	"claudecontrol/internal/settings"
)

// tunable is the part of a renderer the menu can reach. Only the sphere has
// numbers worth a slider; the other two are chosen, not tuned.
type tunable interface {
	Params() holo.Params
	SetParams(holo.Params)
}

// Settings publishes what the menu can change.
func (m *Module) Settings() []settings.Setting {
	out := []settings.Setting{{
		Key:     "style",
		Label:   "renderer",
		Kind:    settings.KindChoice,
		Choices: []string{"sphere", "ring", "avatar"},
		Get:     func() any { return m.style },
		Set:     func(v any) error { return m.setStyle(v.(string)) },
	}, {
		Key:     "readout",
		Label:   "text column",
		Kind:    settings.KindChoice,
		Choices: []string{"right", "left", "off"},
		Get:     func() any { return m.readoutSide() },
		Set:     func(v any) error { return m.setReadout(v.(string)) },
	}}

	// Ranges come from what looks right, not from what the maths allows: a
	// slider whose useful part is a tenth of its travel is a slider nobody can
	// set.
	num := func(key, label string, min, max, step float64,
		get func(holo.Params) float64, set func(*holo.Params, float64)) settings.Setting {
		return settings.Setting{
			Key: key, Label: label, Kind: settings.KindSlider,
			Min: min, Max: max, Step: step,
			Get: func() any { return get(m.tuning()) },
			Set: func(v any) error {
				p := m.tuning()
				set(&p, v.(float64))
				m.setTuning(p)
				return nil
			},
		}
	}

	return append(out,
		num("speed", "flow speed", 0.02, 1.00, 0.01,
			func(p holo.Params) float64 { return p.Speed },
			func(p *holo.Params, v float64) { p.Speed = v }),
		num("trail", "trail persistence", 0.50, 0.99, 0.01,
			func(p holo.Params) float64 { return p.Trail },
			func(p *holo.Params, v float64) { p.Trail = v }),
		num("density", "density", 0.25, 3.00, 0.05,
			func(p holo.Params) float64 { return p.Density },
			func(p *holo.Params, v float64) { p.Density = v }),
		num("rotation", "rotation", 0.00, 0.50, 0.01,
			func(p holo.Params) float64 { return p.Rotation },
			func(p *holo.Params, v float64) { p.Rotation = v }),
		num("breath", "breathing period", 0.00, 30.0, 0.5,
			func(p holo.Params) float64 { return p.Breath },
			func(p *holo.Params, v float64) { p.Breath = v }),
	)
}

// tuning is the current parameters, from the renderer when it has them and
// from the module's own copy when it does not. The copy is what lets the ring
// and the avatar be visited without losing the sphere's settings.
func (m *Module) tuning() holo.Params {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tuningLocked()
}

func (m *Module) tuningLocked() holo.Params {
	if t, ok := m.renderer.(tunable); ok {
		return t.Params()
	}
	return m.params
}

// setTuning records the parameters and hands them to the renderer if it can
// use them.
func (m *Module) setTuning(p holo.Params) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.params = p
	if t, ok := m.renderer.(tunable); ok {
		t.SetParams(p)
	}
}

// readoutSide is where the text column sits.
func (m *Module) readoutSide() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.side
}

// setReadout moves the column, or takes it away. The sphere is resized as
// part of the same change: it has to be built for the width it will be painted
// into.
func (m *Module) setReadout(side string) error {
	switch side {
	case "off", "left", "right":
	default:
		return fmt.Errorf("hologram: unknown readout %q (want off, left or right)", side)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.side = side
	m.renderer.Resize(m.cols-columnW(m.cols, side), m.rows)
	return nil
}

// Values is what gets written back to the configuration.
func (m *Module) Values() map[string]any {
	p := m.tuning()
	return map[string]any{
		"style":    m.style,
		"readout":  m.side,
		"speed":    p.Speed,
		"trail":    p.Trail,
		"density":  p.Density,
		"rotation": p.Rotation,
		"breath":   p.Breath,
	}
}

// setStyle swaps the renderer, carrying the tuning across so that looking at
// the ring and coming back does not reset the sphere.
func (m *Module) setStyle(style string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if style == m.style {
		return nil
	}
	params := m.tuningLocked()
	var r Renderer
	switch style {
	case "sphere":
		r = newSphere(params)
	case "ring":
		r = newRing()
	case "avatar":
		r = newAvatar()
	default:
		return fmt.Errorf("hologram: unknown style %q (want sphere, ring or avatar)", style)
	}
	r.Resize(m.cols-columnW(m.cols, m.side), m.rows)
	r.SetSignal(m.sig)
	m.renderer, m.style, m.params = r, style, params
	return nil
}
