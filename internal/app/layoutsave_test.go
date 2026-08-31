package app

import (
	"testing"

	"claudecontrol/internal/config"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
)

// describing is a module that can say what it holds, the way a pane of tabs
// can.
type describing struct {
	stub
	values map[string]any
}

func (d *describing) Values() map[string]any { return d.values }

// The arrangement is written in the shape the configuration file uses, so what
// comes back is built by the same code that builds what you wrote.
func TestTheLayoutIsWrittenAsConfiguration(t *testing.T) {
	a := newTestApp(t)
	a.root = &layout.Node{
		Kind: layout.KindSplit, Orientation: layout.Horizontal,
		Ratios: []int{3, 2},
		Children: []*layout.Node{
			{Kind: layout.KindLeaf, PaneID: 1},
			{Kind: layout.KindLeaf, PaneID: 2},
		},
	}
	a.modules[2] = &stub{}
	a.paneSpecs = map[layout.PaneID]config.PaneSpec{
		1: {Module: "term", Options: map[string]any{"cmd": []any{"sh"}}},
		2: {Module: "supervisor", Options: map[string]any{"services": []any{"one"}}},
	}

	got := a.layoutSpec()
	if got == nil {
		t.Fatal("nothing was written")
	}
	if got["split"] != "horizontal" {
		t.Errorf("split = %v, want horizontal", got["split"])
	}
	if ratios, _ := got["ratios"].([]int); len(ratios) != 2 || ratios[0] != 3 || ratios[1] != 2 {
		t.Errorf("ratios = %v, want the ones the dividers were left at", got["ratios"])
	}
	children, _ := got["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("%d children, want 2", len(children))
	}
	first, _ := children[0].(map[string]any)
	if first["module"] != "term" {
		t.Errorf("the first pane is %v, want term", first["module"])
	}

	// The case this memory exists for: a supervisor cannot describe itself,
	// and a pane recorded by name alone would come back with no services.
	second, _ := children[1].(map[string]any)
	if second["module"] != "supervisor" {
		t.Fatalf("the second pane is %v, want supervisor", second["module"])
	}
	opts, _ := second["options"].(map[string]any)
	if opts == nil || opts["services"] == nil {
		t.Errorf("the supervisor lost its services: %v", second["options"])
	}
}

// A module that can describe itself overrides what it was born with: a pane of
// tabs holds what it holds now, not what it was handed at startup.
func TestAPaneThatCanDescribeItselfIsAskedRatherThanRemembered(t *testing.T) {
	a := newTestApp(t)
	a.root = &layout.Node{Kind: layout.KindLeaf, PaneID: 1}
	a.modules[1] = &describing{values: map[string]any{
		"tabs": []any{map[string]any{"title": "moved here", "module": "claude"}},
	}}
	a.paneSpecs = map[layout.PaneID]config.PaneSpec{
		1: {Module: "tabs", Options: map[string]any{
			"tabs": []any{map[string]any{"title": "as configured", "module": "claude"}},
		}},
	}

	got := a.layoutSpec()
	if got == nil || got["options"] == nil {
		t.Fatal("nothing was written")
	}
	opts, _ := got["options"].(map[string]any)
	tabs, _ := opts["tabs"].([]any)
	if len(tabs) != 1 {
		t.Fatalf("options = %v", got["options"])
	}
	first, _ := tabs[0].(map[string]any)
	if first["title"] != "moved here" {
		t.Errorf("the pane was remembered rather than asked: %v", first)
	}
}

// A pane nobody recorded is still a pane. It is written by the name it is
// registered under, which is the most that can honestly be said about it.
func TestAPaneWithNoRecordedSpecIsWrittenByName(t *testing.T) {
	a := newTestApp(t)
	a.root = &layout.Node{Kind: layout.KindLeaf, PaneID: 1}
	a.moduleNames[1] = "hologram"

	got := a.layoutSpec()
	if got == nil || got["module"] != "hologram" {
		t.Fatalf("got %+v, want a hologram", got)
	}
}

var _ module.Module = (*describing)(nil)
