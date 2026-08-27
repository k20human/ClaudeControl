package app

import (
	"strings"
	"testing"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
)

// holder is a stub that says which conversations it holds, and remembers being
// asked to bring one to the front.
type holder struct {
	stub
	ids      []string
	selected string
}

func (h *holder) Sessions() []string { return h.ids }

func (h *holder) SelectSession(id string) bool {
	for _, held := range h.ids {
		if held == id {
			h.selected = id
			return true
		}
	}
	return false
}

// idOnly holds one conversation and publishes it the way a shell does: as what
// is hosted, not as something worth bringing back.
type idOnly struct {
	stub
	id string
}

func (i *idOnly) SessionID() string { return i.id }

func waitingApp(t *testing.T, mods map[layout.PaneID]module.Module) *App {
	t.Helper()
	isolateState(t)
	children := make([]*layout.Node, 0, len(mods))
	rects := map[layout.PaneID]layout.Rect{}
	for id := layout.PaneID(1); int(id) <= len(mods); id++ {
		children = append(children, &layout.Node{Kind: layout.KindLeaf, PaneID: id})
		rects[id] = layout.Rect{X: 0, Y: 0, W: 40, H: 20}
	}
	return &App{
		root: &layout.Node{
			Kind: layout.KindSplit, Orientation: layout.Horizontal,
			Ratios: make([]int, len(mods)), Children: children,
		},
		modules:  mods,
		bus:      testBus,
		pool:     pool.New(testBus),
		rects:    rects,
		area:     layout.Rect{X: 0, Y: 0, W: 80, H: 24},
		focus:    1,
		prev:     1,
		nextPane: layout.PaneID(len(mods)),
		hoverDiv: -1,
		hoverBtn: -1,
		wake:     make(chan struct{}, 1),
	}
}

// markWaiting puts a session in the pool and says it is waiting on you.
func markWaiting(t *testing.T, p *pool.Pool, id string) {
	t.Helper()
	s, err := session.Start(session.Spec{
		ID: session.ID(id), Argv: []string{"sleep", "30"}, Dir: ".", Width: 20, Height: 5,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	p.Add(s, id, ".")
	p.SetState(s.ID, pool.StateWaiting)
}

func TestGoingToWhatIsWaitingCrossesPanes(t *testing.T) {
	right := &idOnly{id: "right-one"}
	a := waitingApp(t, map[layout.PaneID]module.Module{
		1: &idOnly{id: "left-one"},
		2: right,
	})
	markWaiting(t, a.pool, "right-one")

	a.focusWaiting()
	if a.focus != 2 {
		t.Fatalf("focus = %d, want the pane holding the waiting session", a.focus)
	}
}

// A pane of tabs may be showing a different conversation than the one that
// called: focusing the pane without changing the tab leaves you looking at the
// wrong one.
func TestGoingToWhatIsWaitingSelectsTheTab(t *testing.T) {
	tabs := &holder{ids: []string{"tab-a", "tab-b", "tab-c"}}
	a := waitingApp(t, map[layout.PaneID]module.Module{
		1: &idOnly{id: "elsewhere"},
		2: tabs,
	})
	markWaiting(t, a.pool, "tab-b")

	a.focusWaiting()
	if a.focus != 2 {
		t.Fatalf("focus = %d, want the pane of tabs", a.focus)
	}
	if tabs.selected != "tab-b" {
		t.Errorf("the tab brought forward was %q, want tab-b", tabs.selected)
	}
}

// Pressed again it goes to the next one: several sessions waiting are visited
// in turn rather than the same one twice.
func TestGoingToWhatIsWaitingCycles(t *testing.T) {
	a := waitingApp(t, map[layout.PaneID]module.Module{
		1: &idOnly{id: "one"},
		2: &idOnly{id: "two"},
		3: &idOnly{id: "three"},
	})
	markWaiting(t, a.pool, "two")
	markWaiting(t, a.pool, "three")

	a.focusWaiting()
	first := a.focus
	a.focusWaiting()
	if a.focus == first {
		t.Errorf("both presses landed on pane %d; the second should move on", first)
	}
	a.focusWaiting()
	if a.focus != first {
		t.Errorf("the third press landed on %d, want back at %d", a.focus, first)
	}
}

// With nothing waiting it says so rather than moving you somewhere arbitrary.
func TestGoingToWhatIsWaitingWithNothingWaiting(t *testing.T) {
	a := waitingApp(t, map[layout.PaneID]module.Module{
		1: &idOnly{id: "one"},
		2: &idOnly{id: "two"},
	})
	a.focusWaiting()
	if a.focus != 1 {
		t.Errorf("focus moved to %d with nothing waiting", a.focus)
	}
	if !strings.Contains(a.status, "nothing is waiting") {
		t.Errorf("the bar says %q", a.status)
	}
}
