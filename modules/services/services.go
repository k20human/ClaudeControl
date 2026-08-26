// Package services shows whether the things you depend on are up.
package services

import (
	"fmt"
	"image/color"
	"strings"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/module"
	"claudecontrol/internal/probe"
	"claudecontrol/internal/render"
)

func init() { module.Register("services", New) }

var (
	bgPanel   = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	fgUp      = color.RGBA{R: 0x6c, G: 0xc4, B: 0x8a, A: 0xff}
	fgDown    = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
	fgUnknown = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
)

// Module polls a set of probes and lists what they said.
type Module struct {
	probes   []probe.Probe
	interval time.Duration
	runner   *probe.Runner

	mu      sync.RWMutex
	results []probe.Result
}

// New builds the module from its configuration.
func New(cfg map[string]any) (module.Module, error) {
	m := &Module{interval: 5 * time.Second}
	if v, ok := toFloat(cfg["interval"]); ok && v > 0 {
		m.interval = time.Duration(v * float64(time.Second))
	}
	raw, _ := cfg["checks"].([]any)
	for _, item := range raw {
		spec, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("services: each check must be a mapping, got %T", item)
		}
		p, err := probeFrom(spec)
		if err != nil {
			return nil, err
		}
		m.probes = append(m.probes, p)
	}
	return m, nil
}

// probeFrom turns one configured check into a probe.
func probeFrom(spec map[string]any) (probe.Probe, error) {
	name, _ := spec["name"].(string)
	if name == "" {
		return probe.Probe{}, fmt.Errorf("services: a check has no %q", "name")
	}
	if url, ok := spec["http"].(string); ok && url != "" {
		return probe.HTTP(name, url), nil
	}
	if addr, ok := spec["tcp"].(string); ok && addr != "" {
		return probe.TCP(name, addr), nil
	}
	if argv, ok := toStrings(spec["cmd"]); ok && len(argv) > 0 {
		return probe.Command(name, argv), nil
	}
	return probe.Probe{}, fmt.Errorf("services: check %q has none of %q, %q or %q",
		name, "cmd", "http", "tcp")
}

// Init starts the polling and follows what it publishes.
func (m *Module) Init(ctx module.Context) error {
	m.runner = probe.NewRunner(ctx.Bus, m.interval, 3*time.Second)
	for _, p := range m.probes {
		m.runner.Add(p)
	}
	if ctx.Bus != nil {
		ch := ctx.Bus.SubscribeState(probe.HealthTopic)
		go func() {
			for v := range ch {
				if results, ok := v.([]probe.Result); ok {
					m.mu.Lock()
					m.results = results
					m.mu.Unlock()
					if ctx.Wake != nil {
						ctx.Wake()
					}
				}
			}
		}()
	}
	m.runner.Start()
	return nil
}

// Results are the latest answers.
func (m *Module) Results() []probe.Result {
	m.mu.RLock()
	results := m.results
	m.mu.RUnlock()
	if len(results) > 0 {
		return results
	}
	// Before the first round the runner already holds one unknown per probe,
	// which is what the pane should show rather than nothing at all.
	if m.runner != nil {
		return m.runner.Results()
	}
	return nil
}

// Resize records nothing: the list draws to whatever area it is given.
func (m *Module) Resize(int, int) error { return nil }

// Row renders one result, clipped to width.
func Row(r probe.Result, width int) string {
	parts := []string{r.Name, r.Health.String()}
	if r.Detail != "" && r.Health != probe.HealthUp {
		parts = append(parts, r.Detail)
	}
	line := strings.Join(parts, "  ")
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	return line
}

// Draw paints the list.
func (m *Module) Draw(scr uv.Screen, area uv.Rectangle) {
	render.Fill(scr, area, bgPanel)
	results := m.Results()
	if len(results) == 0 {
		render.Text(scr, area.Min.X, area.Min.Y, "no checks configured", fgUnknown, bgPanel)
		return
	}
	for i, r := range results {
		y := area.Min.Y + i
		if y >= area.Max.Y {
			return
		}
		fg := color.Color(fgUnknown)
		switch r.Health {
		case probe.HealthUp:
			fg = fgUp
		case probe.HealthDown:
			fg = fgDown
		}
		render.Text(scr, area.Min.X, y, Row(r, area.Dx()), fg, bgPanel)
	}
}

// Close stops the polling.
func (m *Module) Close() error {
	if m.runner != nil {
		m.runner.Stop()
	}
	return nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

func toStrings(v any) ([]string, bool) {
	switch list := v.(type) {
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	case []string:
		return list, true
	}
	return nil, false
}
