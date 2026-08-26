package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"

	// Registered for its side effect: only the command imports the module
	// types, and these tests need one that publishes settings.
	_ "claudecontrol/modules/hologram"
)

func settingsApp(t *testing.T, cfgPath string) *App {
	t.Helper()
	isolateState(t)
	b := bus.New()
	m, err := module.New("hologram", map[string]any{"style": "sphere"})
	if err != nil {
		t.Fatalf("module.New: %v", err)
	}
	if err := m.Init(module.Context{PaneID: 1, Wake: func() {}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return &App{
		root:        &layout.Node{Kind: layout.KindLeaf, PaneID: 1},
		modules:     map[layout.PaneID]module.Module{1: m},
		moduleNames: map[layout.PaneID]string{1: "hologram"},
		bus:         b,
		pool:        pool.New(b),
		rects:       map[layout.PaneID]layout.Rect{1: {X: 0, Y: 0, W: 80, H: 24}},
		area:        layout.Rect{X: 0, Y: 0, W: 80, H: 24},
		focus:       1,
		prev:        1,
		nextPane:    1,
		cfgPath:     cfgPath,
		hoverDiv:    -1,
		hoverBtn:    -1,
		pointerX:    -1,
		pointerY:    -1,
		wake:        make(chan struct{}, 1),
	}
}

func TestTheSettingsPanelIsReversible(t *testing.T) {
	a := settingsApp(t, filepath.Join(t.TempDir(), "config.yaml"))
	a.toggleSettingsPanel()
	if a.settingsPanel == nil {
		t.Fatal("toggling did not open the panel")
	}
	a.toggleSettingsPanel()
	if a.settingsPanel != nil {
		t.Fatal("toggling again did not close it")
	}
}

// Saving must land in the file, keeping what was written by hand.
func TestSavingWritesTheChangedSettingAndKeepsComments(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	body := "# my file\nlayout:\n  module: hologram\n  options:\n    style: sphere\n    speed: 0.18\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a := settingsApp(t, p)
	a.toggleSettingsPanel()

	for _, s := range a.settingsPanel.Items() {
		if s.Key == "speed" {
			if err := s.Apply(0.44); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := a.saveSettings(); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if !strings.Contains(out, "0.44") {
		t.Errorf("the new value is missing:\n%s", out)
	}
	if !strings.Contains(out, "# my file") {
		t.Errorf("the leading comment was lost:\n%s", out)
	}
}

// The layout the user arranged has to come back next time.
func TestSavingRecordsTheCurrentLayout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	a := settingsApp(t, p)
	a.root = &layout.Node{
		Kind:        layout.KindSplit,
		Orientation: layout.Vertical,
		Ratios:      []int{3, 1},
		Children: []*layout.Node{
			{Kind: layout.KindLeaf, PaneID: 1},
			{Kind: layout.KindLeaf, PaneID: 1},
		},
	}
	a.toggleSettingsPanel()
	if err := a.saveSettings(); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}
	got, _ := os.ReadFile(p)
	out := string(got)
	if !strings.Contains(out, "vertical") {
		t.Errorf("the orientation was not recorded:\n%s", out)
	}
	if !strings.Contains(out, "3") || !strings.Contains(out, "ratios") {
		t.Errorf("the ratios were not recorded:\n%s", out)
	}
}

// A pane with nothing to configure says so rather than opening an empty menu.
func TestAPaneWithNothingToConfigureSaysSo(t *testing.T) {
	a := settingsApp(t, filepath.Join(t.TempDir(), "config.yaml"))
	a.modules[1] = &stub{}
	a.toggleSettingsPanel()
	if a.settingsPanel != nil {
		t.Fatal("an empty menu was opened")
	}
	if !strings.Contains(a.status, "nothing to configure") {
		t.Errorf("status = %q, want it to explain why", a.status)
	}
}
