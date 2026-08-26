package config_test

import (
	"path/filepath"
	"testing"

	"claudecontrol/internal/config"
	"claudecontrol/internal/module"

	// Registered for their side effect: an example naming a module that does
	// not exist should fail here rather than in front of somebody trying it.
	_ "claudecontrol/modules/claude"
	_ "claudecontrol/modules/hologram"
	_ "claudecontrol/modules/services"
	_ "claudecontrol/modules/sessions"
	_ "claudecontrol/modules/tasks"
	_ "claudecontrol/modules/term"
)

// Every shipped example must parse and build. They are documentation, and
// documentation that no longer runs is worse than none.
func TestTheShippedExamplesAllBuild(t *testing.T) {
	paths, err := filepath.Glob("../../examples/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no examples found; the glob is wrong or they were removed")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			root, panes, err := config.Build(cfg)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if root == nil || len(panes) == 0 {
				t.Fatal("the example produced no panes")
			}
			for id, spec := range panes {
				m, err := module.New(spec.Module, spec.Options)
				if err != nil {
					t.Errorf("pane %d (%s): %v", id, spec.Module, err)
					continue
				}
				_ = m.Close()
			}
		})
	}
}
