package hologram_test

import (
	"testing"

	"claudecontrol/internal/module"
	"claudecontrol/internal/settings"
)

func provider(t *testing.T, cfg map[string]any) module.Provider {
	t.Helper()
	m, err := module.New("hologram", cfg)
	if err != nil {
		t.Fatalf("module.New: %v", err)
	}
	if err := m.Init(module.Context{PaneID: 1, Wake: func() {}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	p, ok := m.(module.Provider)
	if !ok {
		t.Fatal("the hologram module publishes no settings")
	}
	return p
}

// The settings the user validated by eye must be the ones the menu shows.
func TestSettingsExposeTheValidatedParameters(t *testing.T) {
	p := provider(t, map[string]any{"style": "sphere"})
	byKey := map[string]settings.Setting{}
	for _, s := range p.Settings() {
		byKey[s.Key] = s
	}
	for _, want := range []struct {
		key   string
		value float64
	}{
		{"speed", 0.18}, {"trail", 0.90}, {"density", 1.0},
		{"rotation", 0.09}, {"breath", 9.0},
	} {
		s, ok := byKey[want.key]
		if !ok {
			t.Errorf("no setting named %q", want.key)
			continue
		}
		if got := s.Float(); got != want.value {
			t.Errorf("%s = %g, want the validated %g", want.key, got, want.value)
		}
	}
	if _, ok := byKey["style"]; !ok {
		t.Error("the renderer cannot be chosen from the menu")
	}
}

// Moving a slider changes the animation there and then.
func TestApplyingASettingReachesTheRenderer(t *testing.T) {
	p := provider(t, map[string]any{"style": "sphere"})
	var speed settings.Setting
	for _, s := range p.Settings() {
		if s.Key == "speed" {
			speed = s
		}
	}
	if err := speed.Apply(0.5); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := speed.Float(); got != 0.5 {
		t.Fatalf("speed = %g after applying 0.5", got)
	}
}

// Switching style from the menu must actually switch renderer.
func TestApplyingTheStyleSwapsTheRenderer(t *testing.T) {
	p := provider(t, map[string]any{"style": "sphere"})
	var style settings.Setting
	for _, s := range p.Settings() {
		if s.Key == "style" {
			style = s
		}
	}
	if err := style.Apply("avatar"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := style.String(); got != "avatar" {
		t.Fatalf("style = %q, want avatar", got)
	}
	if err := style.Apply("nonsense"); err == nil {
		t.Error("an unlisted style was accepted")
	}
}

// Values are what gets written back to the configuration.
func TestValuesReportWhatTheMenuChanged(t *testing.T) {
	p := provider(t, map[string]any{"style": "sphere"})
	m, ok := p.(interface{ Values() map[string]any })
	if !ok {
		t.Fatal("the module cannot report its values")
	}
	for _, s := range p.Settings() {
		if s.Key == "trail" {
			if err := s.Apply(0.55); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := m.Values()
	if got["trail"] != 0.55 {
		t.Errorf("Values()[trail] = %v, want 0.55", got["trail"])
	}
	if got["style"] != "sphere" {
		t.Errorf("Values()[style] = %v, want sphere", got["style"])
	}
}

// Looking at another renderer and coming back must not reset the tuning.
func TestTuningSurvivesAStyleRoundTrip(t *testing.T) {
	p := provider(t, map[string]any{"style": "sphere"})
	find := func(key string) settings.Setting {
		for _, s := range p.Settings() {
			if s.Key == key {
				return s
			}
		}
		t.Fatalf("no setting named %q", key)
		return settings.Setting{}
	}
	if err := find("speed").Apply(0.61); err != nil {
		t.Fatal(err)
	}
	if err := find("style").Apply("ring"); err != nil {
		t.Fatal(err)
	}
	if err := find("style").Apply("sphere"); err != nil {
		t.Fatal(err)
	}
	if got := find("speed").Float(); got != 0.61 {
		t.Fatalf("speed = %g after a round trip through the ring, want 0.61", got)
	}
}
