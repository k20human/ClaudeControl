package app

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
)

// isolateState points the snapshot at a directory of its own. Laying panes out
// records them, and a unit test must not write into the state of the person
// running it.
func isolateState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// testBus backs the pool the lifecycle fixtures use.
var testBus = bus.New()

// stub is a module that draws nothing, so lifecycle can be tested without a
// terminal or a process.
type stub struct{ closed bool }

func (s *stub) Init(module.Context) error    { return nil }
func (s *stub) Resize(int, int) error        { return nil }
func (s *stub) Draw(uv.Screen, uv.Rectangle) {}
func (s *stub) Close() error                 { s.closed = true; return nil }

func newTestApp(t *testing.T) *App {
	isolateState(t)
	return &App{
		root:     &layout.Node{Kind: layout.KindLeaf, PaneID: 1},
		modules:  map[layout.PaneID]module.Module{1: &stub{}},
		bus:      testBus,
		pool:     pool.New(testBus),
		rects:    map[layout.PaneID]layout.Rect{1: {X: 0, Y: 0, W: 80, H: 24}},
		area:     layout.Rect{X: 0, Y: 0, W: 80, H: 24},
		focus:    1,
		prev:     1,
		nextPane: 1,
		hoverDiv: -1,
		wake:     make(chan struct{}, 1),
	}
}

func TestClosePaneRemovesTheLeafAndClosesTheModule(t *testing.T) {
	a := newTestApp(t)
	a.root, _ = layout.Split(a.root, 1, &layout.Node{Kind: layout.KindLeaf, PaneID: 2}, layout.Horizontal)
	m2 := &stub{}
	a.modules[2] = m2
	a.nextPane = 2
	a.rects = layout.Compute(a.root, a.area)

	if err := a.closePane(2); err != nil {
		t.Fatalf("closePane: %v", err)
	}
	if !m2.closed {
		t.Error("module 2 was not closed")
	}
	if _, still := a.modules[2]; still {
		t.Error("module 2 is still registered")
	}
	if ids := layout.Leaves(a.root); len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("Leaves = %v, want [1]", ids)
	}
	if a.focus != 1 {
		t.Errorf("focus = %d, want 1", a.focus)
	}
}

func TestClosingTheLastPaneQuits(t *testing.T) {
	a := newTestApp(t)
	if err := a.closePane(1); err != nil {
		t.Fatalf("closePane: %v", err)
	}
	if !a.quit {
		t.Error("closing the last pane did not set quit")
	}
}

func TestToggleZoomIsReversible(t *testing.T) {
	a := newTestApp(t)
	a.root, _ = layout.Split(a.root, 1, &layout.Node{Kind: layout.KindLeaf, PaneID: 2}, layout.Horizontal)
	a.modules[2] = &stub{}
	a.nextPane = 2
	before := layout.Compute(a.root, a.paneArea())

	a.toggleZoom()
	if a.zoomed != 1 {
		t.Fatalf("zoomed = %d, want 1", a.zoomed)
	}
	if got := a.rects[1]; got != a.paneArea() {
		t.Fatalf("zoomed pane rect = %+v, want the pane area %+v", got, a.paneArea())
	}

	a.toggleZoom()
	if a.zoomed != 0 {
		t.Fatalf("zoomed = %d after the second toggle, want 0", a.zoomed)
	}
	after := layout.Compute(a.root, a.paneArea())
	if before[1] != after[1] || before[2] != after[2] {
		t.Fatalf("layout changed across a zoom cycle: %v then %v", before, after)
	}
}

func TestClosingAZoomedPaneLeavesZoom(t *testing.T) {
	a := newTestApp(t)
	a.root, _ = layout.Split(a.root, 1, &layout.Node{Kind: layout.KindLeaf, PaneID: 2}, layout.Horizontal)
	a.modules[2] = &stub{}
	a.nextPane = 2
	a.rects = layout.Compute(a.root, a.area)
	a.setFocus(2)
	a.toggleZoom()

	if err := a.closePane(2); err != nil {
		t.Fatalf("closePane: %v", err)
	}
	if a.zoomed != 0 {
		t.Errorf("zoomed = %d after closing the zoomed pane, want 0", a.zoomed)
	}
	if _, ok := a.rects[1]; !ok {
		t.Error("the surviving pane has no rectangle")
	}
}
