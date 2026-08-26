package app

import (
	"testing"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
)

func panelApp() *App {
	b := bus.New()
	return &App{
		root:     &layout.Node{Kind: layout.KindLeaf, PaneID: 1},
		modules:  map[layout.PaneID]module.Module{1: &stub{}},
		bus:      b,
		pool:     pool.New(b),
		rects:    map[layout.PaneID]layout.Rect{1: {X: 0, Y: 0, W: 80, H: 24}},
		area:     layout.Rect{X: 0, Y: 0, W: 80, H: 24},
		focus:    1,
		prev:     1,
		nextPane: 1,
		hoverDiv: -1,
		hoverBtn: -1,
		pointerX: -1,
		pointerY: -1,
		wake:     make(chan struct{}, 1),
	}
}

func TestToggleSessionPanelIsReversible(t *testing.T) {
	a := panelApp()
	if a.sessionPanel != nil {
		t.Fatal("the panel is open before anything asked for it")
	}
	a.toggleSessionPanel()
	if a.sessionPanel == nil {
		t.Fatal("toggling did not open the panel")
	}
	a.toggleSessionPanel()
	if a.sessionPanel != nil {
		t.Fatal("toggling again did not close the panel")
	}
}

// The panel is a view, not a pane: opening it must not disturb the layout the
// user arranged.
func TestTheSessionPanelDoesNotTouchTheLayout(t *testing.T) {
	a := panelApp()
	before := layout.Leaves(a.root)
	a.toggleSessionPanel()
	if got := layout.Leaves(a.root); len(got) != len(before) {
		t.Fatalf("Leaves = %v, want the layout untouched %v", got, before)
	}
	if a.focus != 1 {
		t.Errorf("focus = %d, want it to stay on the pane", a.focus)
	}
}
