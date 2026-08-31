package supervisor_test

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/modules/supervisor"
)

// one is what a pane offers for acting on a single service.
type one interface {
	StartOne(int)
	StopOne(int)
	RestartOne(int)
}

// A service is started on its own, whatever else is ticked. The buttons at the
// top act on the ticked set; a row acts on itself.
func TestOneServiceStartsWithoutTheOthers(t *testing.T) {
	m := build(t, two())
	acts, ok := any(m).(one)
	if !ok {
		t.Fatal("a service cannot be acted on by itself")
	}

	acts.StartOne(0)
	waitFor(t, "the first", func() bool { return stateOf(m, "alpha").State == supervisor.Running })
	if got := stateOf(m, "beta").State; got != supervisor.Stopped {
		t.Errorf("beta is %v; starting one must not start the other", got)
	}
}

// And stopped on its own, leaving the rest of the stack up.
func TestOneServiceStopsWithoutTheOthers(t *testing.T) {
	m := build(t, two())
	m.StartPicked()
	waitFor(t, "both", func() bool {
		return stateOf(m, "alpha").State == supervisor.Running &&
			stateOf(m, "beta").State == supervisor.Running
	})

	any(m).(one).StopOne(0)
	waitFor(t, "the first to stop", func() bool {
		return stateOf(m, "alpha").State == supervisor.Stopped
	})
	if got := stateOf(m, "beta").State; got != supervisor.Running {
		t.Errorf("beta is %v; stopping one must not stop the other", got)
	}
}

// Restarted on its own, and it is a new process.
func TestOneServiceRestartsWithoutTheOthers(t *testing.T) {
	m := build(t, two())
	m.StartPicked()
	waitFor(t, "both", func() bool {
		return stateOf(m, "alpha").State == supervisor.Running &&
			stateOf(m, "beta").State == supervisor.Running
	})
	wasA := stateOf(m, "alpha").Pid
	wasB := stateOf(m, "beta").Pid

	any(m).(one).RestartOne(0)
	waitFor(t, "a new process for the first", func() bool {
		got := stateOf(m, "alpha")
		return got.State == supervisor.Running && got.Pid != wasA
	})
	if got := stateOf(m, "beta").Pid; got != wasB {
		t.Errorf("beta was restarted too (pid %d, was %d)", got, wasB)
	}
}

// An index nobody has is nobody's service.
func TestActingOnNoServiceDoesNothing(t *testing.T) {
	m := build(t, two())
	acts := any(m).(one)
	for _, i := range []int{-1, 2, 99} {
		acts.StartOne(i)
		acts.StopOne(i)
		acts.RestartOne(i)
	}
	for _, s := range m.Services() {
		if s.State != supervisor.Stopped {
			t.Errorf("%s is %v after acting on nothing", s.Name, s.State)
		}
	}
}

// The controls are on the highlighted row and no other. A stray click must not
// stop a server you were not even looking at — the tab strip hides its close
// cross for the same reason.
func TestTheRowControlsAreOnTheHighlightedRowOnly(t *testing.T) {
	m := build(t, two())
	if err := m.Resize(60, 10); err != nil {
		t.Fatal(err)
	}
	g := paint(t, m, 60, 10)

	rows := 0
	for y := 0; y < 10; y++ {
		if strings.Contains(g.row(y), "⟳") && !strings.Contains(g.row(y), "restart") {
			rows++
		}
	}
	if rows != 1 {
		t.Errorf("%d rows carry the controls, want only the highlighted one:\n%s", rows, g.text())
	}
}

// Clicking one acts on that service.
func TestClickingARowControlStartsThatService(t *testing.T) {
	m := build(t, two())
	if err := m.Resize(60, 10); err != nil {
		t.Fatal(err)
	}
	g := paint(t, m, 60, 10)

	// The play glyph on the highlighted row.
	x, y := -1, -1
	for row := 0; row < 10 && x < 0; row++ {
		line := g.row(row)
		if c := strings.Index(line, "▸"); c >= 0 && !strings.Contains(line, "start") {
			x, y = len([]rune(line[:c])), row
		}
	}
	if x < 0 {
		t.Fatalf("no control on the highlighted row:\n%s", g.text())
	}
	m.Mouse(uv.MouseClickEvent{X: x, Y: y, Button: uv.MouseLeft})

	waitFor(t, "the highlighted service", func() bool {
		return stateOf(m, "alpha").State == supervisor.Running
	})
	if got := stateOf(m, "beta").State; got != supervisor.Stopped {
		t.Errorf("beta is %v; the row acts on its own service", got)
	}
}

// The keyboard follows the same split: the letter acts on the row you are on,
// and shifted it acts on everything ticked.
func TestTheLettersActOnTheRowAndShiftOnTheTickedSet(t *testing.T) {
	m := build(t, two())
	keys := any(m).(interface{ Key(uv.KeyEvent) })

	// s on the first row starts that one alone.
	keys.Key(uv.KeyPressEvent{Code: 's', Text: "s"})
	waitFor(t, "the first", func() bool { return stateOf(m, "alpha").State == supervisor.Running })
	if got := stateOf(m, "beta").State; got != supervisor.Stopped {
		t.Fatalf("beta is %v; the letter acts on one row", got)
	}

	// Shifted, on the ticked set — which is both, as they start ticked.
	keys.Key(uv.KeyPressEvent{Code: 'S', Text: "S", Mod: uv.ModShift})
	waitFor(t, "both", func() bool {
		return stateOf(m, "alpha").State == supervisor.Running &&
			stateOf(m, "beta").State == supervisor.Running
	})

	// And x takes down only the row.
	keys.Key(uv.KeyPressEvent{Code: 'x', Text: "x"})
	waitFor(t, "the first to stop", func() bool {
		return stateOf(m, "alpha").State == supervisor.Stopped
	})
	if got := stateOf(m, "beta").State; got != supervisor.Running {
		t.Errorf("beta is %v; x acts on one row", got)
	}
}
