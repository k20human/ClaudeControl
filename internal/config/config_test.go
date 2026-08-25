package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"claudecontrol/internal/config"
	"claudecontrol/internal/layout"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestLoadBuildsATreeWithOnePaneSpecPerLeaf(t *testing.T) {
	p := write(t, `
layout:
  split: horizontal
  ratios: [3, 1]
  children:
    - module: term
      options:
        cmd: [claude]
        dir: /tmp
    - module: term
      options:
        cmd: [bash]
`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	root, panes, err := config.Build(c)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if root.Kind != layout.KindSplit || root.Orientation != layout.Horizontal {
		t.Fatalf("root = %+v, want a horizontal split", root)
	}
	if len(root.Ratios) != 2 || root.Ratios[0] != 3 || root.Ratios[1] != 1 {
		t.Fatalf("Ratios = %v, want [3 1]", root.Ratios)
	}
	ids := layout.Leaves(root)
	if len(ids) != 2 {
		t.Fatalf("Leaves = %v, want 2 leaves", ids)
	}
	if len(panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(panes))
	}
	first := panes[ids[0]]
	if first.Module != "term" {
		t.Fatalf("pane module = %q, want term", first.Module)
	}
	if first.Options["dir"] != "/tmp" {
		t.Fatalf("pane options = %v, want dir=/tmp", first.Options)
	}
}

func TestLoadRejectsARatioCountMismatch(t *testing.T) {
	p := write(t, `
layout:
  split: vertical
  ratios: [1, 1, 1]
  children:
    - module: term
      options: { cmd: [bash] }
    - module: term
      options: { cmd: [bash] }
`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, _, err := config.Build(c); err == nil {
		t.Fatal("Build = nil error, want a ratio/children mismatch error")
	}
}

func TestLoadRejectsAnUnknownSplitAxis(t *testing.T) {
	p := write(t, `
layout:
  split: sideways
  children:
    - module: term
      options: { cmd: [bash] }
`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, _, err := config.Build(c); err == nil {
		t.Fatal("Build = nil error, want an unknown-axis error")
	}
}

// A missing file is not a failure: the application must start on defaults.
func TestLoadMissingFileReturnsTheDefault(t *testing.T) {
	c, err := config.Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	root, panes, err := config.Build(c)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if root == nil || len(panes) != 1 {
		t.Fatalf("default layout = %+v with %d panes, want one pane", root, len(panes))
	}
}

// A malformed file must be reported, not silently replaced by defaults.
func TestLoadMalformedFileIsAnError(t *testing.T) {
	p := write(t, "layout: [this is not a mapping\n")
	if _, err := config.Load(p); err == nil {
		t.Fatal("Load = nil error, want a parse error")
	}
}
