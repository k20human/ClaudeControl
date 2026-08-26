package supervisor_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/module"
	"claudecontrol/modules/supervisor"
)

// grid is a screen that remembers what was painted.
type grid struct {
	w, h  int
	cells []uv.Cell
}

func newGrid(w, h int) *grid {
	g := &grid{w: w, h: h, cells: make([]uv.Cell, w*h)}
	for i := range g.cells {
		g.cells[i] = uv.EmptyCell
	}
	return g
}

func (g *grid) Bounds() uv.Rectangle { return uv.Rect(0, 0, g.w, g.h) }
func (g *grid) CellAt(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return nil
	}
	return &g.cells[y*g.w+x]
}
func (g *grid) SetCell(x, y int, c *uv.Cell) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h || c == nil {
		return
	}
	g.cells[y*g.w+x] = *c
}
func (g *grid) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }

func (g *grid) row(y int) string {
	var b strings.Builder
	for x := 0; x < g.w; x++ {
		b.WriteString(g.cells[y*g.w+x].Content)
	}
	return strings.TrimRight(b.String(), " ")
}

func (g *grid) text() string {
	var b strings.Builder
	for y := 0; y < g.h; y++ {
		b.WriteString(g.row(y))
		b.WriteByte('\n')
	}
	return b.String()
}

func build(t *testing.T, cfg map[string]any) *supervisor.Module {
	t.Helper()
	built, err := module.New("supervisor", cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	m := built.(*supervisor.Module)
	if err := m.Init(module.Context{Wake: func() {}}); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Resize(70, 12); err != nil {
		t.Fatalf("resize: %v", err)
	}
	return m
}

func paint(t *testing.T, m *supervisor.Module, w, h int) *grid {
	t.Helper()
	g := newGrid(w, h)
	m.Draw(g, uv.Rect(0, 0, w, h))
	return g
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func stateOf(m *supervisor.Module, name string) supervisor.Snapshot {
	for _, s := range m.Services() {
		if s.Name == name {
			return s
		}
	}
	return supervisor.Snapshot{Name: "<missing>"}
}

func two() map[string]any {
	return map[string]any{"services": []any{
		map[string]any{"name": "alpha", "cmd": []any{"sh", "-c", "printf ALPHA-UP; sleep 300"}},
		map[string]any{"name": "beta", "cmd": []any{"sh", "-c", "printf BETA-UP; sleep 300"}},
	}}
}

// A pane that spawned a dozen development servers the moment it appeared would
// be a surprise, and the one thing a supervisor must never be is surprising
// about what is running.
func TestNothingStartsUnlessItAsksTo(t *testing.T) {
	m := build(t, two())
	for _, s := range m.Services() {
		if s.State != supervisor.Stopped {
			t.Errorf("%s is %v before anything was asked", s.Name, s.State)
		}
	}

	auto := build(t, map[string]any{"services": []any{
		map[string]any{"name": "eager", "cmd": []any{"sh", "-c", "sleep 300"}, "autostart": true},
	}})
	waitFor(t, "the eager service", func() bool {
		return stateOf(auto, "eager").State == supervisor.Running
	})
}

// The buttons act on what is ticked, and on nothing else.
func TestTheButtonsActOnlyOnWhatIsPicked(t *testing.T) {
	m := build(t, two())
	// The clickable regions are published as the pane is drawn, so what you
	// click is what you saw. Nothing is clickable before the first frame.
	paint(t, m, 70, 8)
	// Untick the second row.
	m.Mouse(clickAt(1, 3))
	if stateOf(m, "beta").Picked {
		t.Fatal("clicking the box did not untick beta")
	}

	m.StartPicked()
	waitFor(t, "alpha", func() bool { return stateOf(m, "alpha").State == supervisor.Running })
	if got := stateOf(m, "beta"); got.State != supervisor.Stopped {
		t.Errorf("beta is %v although it was unticked", got.State)
	}
	if stateOf(m, "alpha").Pid <= 0 {
		t.Error("a running service has no pid")
	}

	m.StopPicked()
	waitFor(t, "alpha to stop", func() bool {
		return stateOf(m, "alpha").State == supervisor.Stopped
	})
}

// A service that dies stays dead and says how. Bringing it back would hide the
// failure behind a row that reads "running" while nothing works.
func TestAServiceThatDiesIsReportedAndNotResurrected(t *testing.T) {
	m := build(t, map[string]any{"services": []any{
		map[string]any{"name": "brief", "cmd": []any{"sh", "-c", "exit 3"}},
	}})
	m.StartPicked()

	waitFor(t, "the exit", func() bool { return stateOf(m, "brief").State == supervisor.Exited })
	if got := stateOf(m, "brief").Code; got != 3 {
		t.Errorf("exit code %d, want 3", got)
	}

	// Still dead a moment later: nothing brought it back.
	time.Sleep(400 * time.Millisecond)
	if got := stateOf(m, "brief").State; got != supervisor.Exited {
		t.Errorf("the service is %v; something restarted it", got)
	}
	if out := paint(t, m, 70, 8).text(); !strings.Contains(out, "exited") ||
		!strings.Contains(out, "code 3") {
		t.Errorf("the pane does not report the failure:\n%s", out)
	}
}

// Quitting must not leave a stack running with nothing left to manage it, and
// a launcher's children have to go with it.
func TestCloseTakesTheWholeStackDown(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child.pid")
	m := build(t, map[string]any{
		"stop_grace": 2,
		"services": []any{
			map[string]any{"name": "launcher", "cmd": []any{
				"sh", "-c", "sleep 300 & echo $! > " + marker + "; wait",
			}},
		},
	})
	m.StartPicked()
	waitFor(t, "the launcher", func() bool {
		return stateOf(m, "launcher").State == supervisor.Running
	})

	var child int
	waitFor(t, "the child's pid", func() bool {
		raw, err := os.ReadFile(marker)
		if err != nil {
			return false
		}
		child, err = strconv.Atoi(strings.TrimSpace(string(raw)))
		return err == nil && child > 0
	})

	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitFor(t, "the child to go", func() bool { return !alive(child) })
}

func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// Clicking a name shows what that service wrote, exactly as it wrote it, and
// back returns to the list.
func TestClickingANameShowsItsOutput(t *testing.T) {
	m := build(t, two())
	m.StartPicked()
	waitFor(t, "alpha", func() bool { return stateOf(m, "alpha").State == supervisor.Running })

	// The name of the first row.
	paint(t, m, 70, 12)
	m.Mouse(clickAt(6, 2))
	if m.Showing() != 0 {
		t.Fatalf("Showing = %d, want 0", m.Showing())
	}
	var out string
	waitFor(t, "the output", func() bool {
		out = paint(t, m, 70, 12).text()
		return strings.Contains(out, "ALPHA-UP")
	})
	if !strings.Contains(out, "logs · alpha") || !strings.Contains(out, "◀ back") {
		t.Errorf("the log view has no header:\n%s", out)
	}

	m.Mouse(clickAt(2, 0))
	if m.Showing() != -1 {
		t.Errorf("back left Showing at %d", m.Showing())
	}
}

// A restart must not start the new process before the old one has let go.
func TestRestartWaitsForTheOldProcessToGo(t *testing.T) {
	m := build(t, two())
	m.StartPicked()
	waitFor(t, "both up", func() bool {
		return stateOf(m, "alpha").State == supervisor.Running &&
			stateOf(m, "beta").State == supervisor.Running
	})
	was := stateOf(m, "alpha").Pid

	m.RestartPicked()
	waitFor(t, "a new process", func() bool {
		got := stateOf(m, "alpha")
		return got.State == supervisor.Running && got.Pid != was
	})
	if alive(was) {
		t.Errorf("the old process %d is still running after the restart", was)
	}
}

func TestConfigurationIsCheckedRatherThanGuessed(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"no services", map[string]any{}, "services"},
		{"no name", map[string]any{"services": []any{
			map[string]any{"cmd": []any{"true"}}}}, "name"},
		{"no cmd", map[string]any{"services": []any{
			map[string]any{"name": "x"}}}, "cmd"},
		{"not a mapping", map[string]any{"services": []any{"x"}}, "mapping"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := module.New("supervisor", c.cfg)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %v does not name %q", err, c.want)
			}
		})
	}

	// A bare 3000 in YAML is a number, and a bare true is a boolean. Refusing
	// them would be an error about a type the author never typed.
	if _, err := module.New("supervisor", map[string]any{"services": []any{
		map[string]any{"name": "x", "cmd": []any{"sh", "-c", true, 3000}},
	}}); err != nil {
		t.Errorf("a scalar in cmd was refused: %v", err)
	}
}

func clickAt(x, y int) uv.MouseEvent {
	return uv.MouseClickEvent{X: x, Y: y, Button: uv.MouseLeft}
}
