package supervisor_test

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/modules/supervisor"
)

// paintAt draws the module somewhere other than the screen's top-left corner,
// which is where every other test draws it and where the two coordinate
// systems happen to agree.
func paintAt(t *testing.T, m *supervisor.Module, x, y, w, h int) *grid {
	t.Helper()
	g := newGrid(x+w+4, y+h+4)
	m.Draw(g, uv.Rect(x, y, w, h))
	return g
}

// The pane is rarely at the top-left corner of the screen: this one lives in a
// tab, in the right-hand half of a split. The application hands a module a
// click in the module's own coordinates, so the regions it records have to be
// in those coordinates too — otherwise every button is dead everywhere except
// in a test.
func TestTheButtonsAnswerWhereverThePaneIsDrawn(t *testing.T) {
	m := build(t, two())
	if err := m.Resize(50, 12); err != nil {
		t.Fatalf("resize: %v", err)
	}
	paintAt(t, m, 37, 2, 50, 12)

	// "▸ start" is the first button, one column in from the left edge of the
	// pane and on its first row.
	m.Mouse(clickAt(2, 0))

	waitFor(t, "the services to start", func() bool {
		return stateOf(m, "alpha").State == supervisor.Running &&
			stateOf(m, "beta").State == supervisor.Running
	})
}

// The same for a service row: clicking its name shows its output, and clicking
// its box ticks it.
func TestARowAnswersWhereverThePaneIsDrawn(t *testing.T) {
	m := build(t, two())
	if err := m.Resize(50, 12); err != nil {
		t.Fatalf("resize: %v", err)
	}
	paintAt(t, m, 37, 2, 50, 12)

	// The first service's row, two below the button row and the rule.
	m.Mouse(clickAt(6, 2))
	if m.Showing() != 0 {
		t.Errorf("Showing = %d; clicking a name should open its output", m.Showing())
	}
}
