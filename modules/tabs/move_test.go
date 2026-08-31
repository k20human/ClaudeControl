package tabs_test

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/module"
)

// mover is what a pane must offer for a tab to leave it or arrive in it.
type mover interface {
	tabber
	Detach(int) (module.Module, module.Held, bool)
	Adopt(module.Module, module.Held) error
	Reorder(int, int)
	Count() int
	ActiveIndex() int
}

func movable(t *testing.T, cfg map[string]any) mover {
	t.Helper()
	m, ok := build(t, cfg, module.Context{Wake: func() {}}).(mover)
	if !ok {
		t.Fatal("a pane of tabs cannot move its tabs")
	}
	return m
}

func oneTab() map[string]any {
	return map[string]any{"tabs": []any{
		map[string]any{"title": "only", "module": "term", "options": shell("printf ONLY; cat")},
	}}
}

// A moved tab keeps the process it was showing. Detaching therefore hands the
// module over rather than closing it: closing it would end the session, which
// is the loss this gesture exists to prevent.
func TestDetachHandsOverAModuleThatIsStillAlive(t *testing.T) {
	m := movable(t, two())
	waitFor(t, "the first tab", func() bool {
		return strings.Contains(paint(t, m, 50, 10).text(), "FIRST")
	})

	mod, h, ok := m.Detach(0)
	if !ok {
		t.Fatal("Detach reported nothing to take")
	}
	if h.Title != "one" {
		t.Errorf("title = %q, want one", h.Title)
	}
	// What it was built from and with travels with it: a term cannot describe
	// itself, and a tab recorded by name alone comes back as a bare shell.
	if h.Name != "term" {
		t.Errorf("name = %q, want term", h.Name)
	}
	if h.Options["cmd"] == nil {
		t.Errorf("the tab lost the command it was built with: %v", h.Options)
	}
	if mod == nil {
		t.Fatal("Detach handed back no module")
	}
	if m.Count() != 1 {
		t.Errorf("%d tabs left, want 1", m.Count())
	}
	t.Cleanup(func() { _ = mod.Close() })

	// Still alive: it draws what it was drawing.
	g := newGrid(50, 10)
	mod.Draw(g, uv.Rect(0, 0, 50, 10))
	if !strings.Contains(g.text(), "FIRST") {
		t.Errorf("the detached module stopped drawing:\n%s", g.text())
	}
}

// And a pane takes one that is already running, without starting anything.
func TestAdoptTakesAModuleThatIsAlreadyRunning(t *testing.T) {
	from := movable(t, two())
	waitFor(t, "the first tab", func() bool {
		return strings.Contains(paint(t, from, 50, 10).text(), "FIRST")
	})
	to := movable(t, oneTab())
	waitFor(t, "its own tab", func() bool {
		return strings.Contains(paint(t, to, 50, 10).text(), "ONLY")
	})

	mod, h, _ := from.Detach(0)
	if err := to.Adopt(mod, h); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if to.Count() != 2 {
		t.Fatalf("%d tabs after adopting, want 2", to.Count())
	}
	if to.ActiveIndex() != 1 {
		t.Errorf("the arriving tab is not the one on screen (active = %d)", to.ActiveIndex())
	}
	waitFor(t, "the arriving tab to draw", func() bool {
		return strings.Contains(paint(t, to, 50, 10).text(), "FIRST")
	})
}

// Taking the last tab empties the pane, which is what tells the application to
// remove it.
func TestDetachingTheLastTabEmptiesThePane(t *testing.T) {
	m := movable(t, oneTab())
	mod, _, ok := m.Detach(0)
	if !ok {
		t.Fatal("Detach refused the only tab")
	}
	t.Cleanup(func() { _ = mod.Close() })
	if m.Count() != 0 {
		t.Errorf("%d tabs left, want none", m.Count())
	}
}

// An index nobody has is not a tab.
func TestDetachingWhatIsNotThereIsRefused(t *testing.T) {
	m := movable(t, two())
	for _, i := range []int{-1, 2, 99} {
		if _, _, ok := m.Detach(i); ok {
			t.Errorf("Detach(%d) took something", i)
		}
	}
}

// Dragging a tab along its own strip moves it in the order, and leaves you
// looking at the tab you dragged.
func TestReorderMovesATabInTheStrip(t *testing.T) {
	m := movable(t, two())
	strip := paint(t, m, 50, 10).row(0)
	if strings.Index(strip, "one") > strings.Index(strip, "two") {
		t.Fatalf("the strip starts as %q", strip)
	}

	m.Reorder(0, 1)
	strip = paint(t, m, 50, 10).row(0)
	if strings.Index(strip, "two") > strings.Index(strip, "one") {
		t.Errorf("after reordering the strip is %q, want two before one", strip)
	}
	if got := m.ActiveTitle(); got != "one" {
		t.Errorf("the active tab is %q, want the one that was moved", got)
	}
}

// A pane of tabs with nothing in it yet: how the application makes one to pour
// tabs into, when a tab is dropped on a pane that was not a pane of tabs.
//
// An empty list is deliberate; a missing one is a mistake in the
// configuration, and the two must not be answered the same way.
func TestAnEmptyListOfTabsIsAllowedAndAMissingOneIsNot(t *testing.T) {
	empty, err := module.New("tabs", map[string]any{"tabs": []any{}})
	if err != nil {
		t.Fatalf("an empty list was refused: %v", err)
	}
	if err := empty.Init(module.Context{Wake: func() {}}); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = empty.Close() })
	if n := empty.(mover).Count(); n != 0 {
		t.Errorf("%d tabs in an empty pane", n)
	}

	if _, err := module.New("tabs", nil); err == nil {
		t.Error("a pane of tabs with no tabs key was accepted")
	}
}
