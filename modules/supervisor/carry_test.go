package supervisor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/module"
	"claudecontrol/modules/supervisor"
)

// oneNoisy is a service that says something and then stays up.
func oneNoisy(marker string) map[string]any {
	return map[string]any{"services": []any{
		map[string]any{"name": "noisy", "cmd": []any{
			"sh", "-c", "printf '" + marker + "\\n'; sleep 300"}},
	}}
}

// showLog puts the pane on a service's output.
func showLog(t *testing.T, m *supervisor.Module) {
	t.Helper()
	paint(t, m, 70, 12)
	m.Mouse(clickAt(6, 2))
	if m.Showing() != 0 {
		t.Fatalf("Showing = %d, want the first service", m.Showing())
	}
}

// You restart a service because of something it said. Throwing that away at
// the moment you act on it is the worst possible time to throw it away.
func TestARestartKeepsWhatTheLastRunPrinted(t *testing.T) {
	m := build(t, oneNoisy("FIRST-RUN-SAID-THIS"))
	m.StartPicked()
	waitFor(t, "the service", func() bool { return stateOf(m, "noisy").State == supervisor.Running })

	showLog(t, m)
	waitFor(t, "its output", func() bool {
		return strings.Contains(paint(t, m, 70, 12).text(), "FIRST-RUN-SAID-THIS")
	})

	was := stateOf(m, "noisy").Pid
	m.RestartPicked()
	waitFor(t, "a new process", func() bool {
		got := stateOf(m, "noisy")
		return got.State == supervisor.Running && got.Pid != was
	})

	var out string
	waitFor(t, "the previous run", func() bool {
		out = paint(t, m, 70, 12).text()
		return strings.Contains(out, "previous run")
	})
	if !strings.Contains(out, "FIRST-RUN-SAID-THIS") {
		t.Errorf("the restart lost what the last run said:\n%s", out)
	}
}

// And it survives the application closing, which is the other half of the same
// promise: what a service said is a record, and a record that only lasts as
// long as the window is not one.
func TestTheOutputSurvivesTheApplicationClosing(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	first, err := module.New("supervisor", oneNoisy("SAID-BEFORE-QUITTING"))
	if err != nil {
		t.Fatal(err)
	}
	one := first.(*supervisor.Module)
	if err := one.Init(module.Context{Wake: func() {}}); err != nil {
		t.Fatal(err)
	}
	if err := one.Resize(70, 12); err != nil {
		t.Fatal(err)
	}
	one.StartPicked()
	waitFor(t, "the service", func() bool { return stateOf(one, "noisy").State == supervisor.Running })
	showLog(t, one)
	waitFor(t, "its output", func() bool {
		return strings.Contains(paint(t, one, 70, 12).text(), "SAID-BEFORE-QUITTING")
	})
	_ = one.Close()

	kept, err := os.ReadDir(filepath.Join(state, "claudecontrol", "services"))
	if err != nil || len(kept) != 1 {
		t.Fatalf("nothing was written for the next run: %v %v", kept, err)
	}
	if !strings.HasPrefix(kept[0].Name(), "noisy-") {
		t.Errorf("the file is called %q; the service's name should still be in it", kept[0].Name())
	}

	// buildIn, not build: the point of this test is the directory the first
	// module wrote to, and build would hand the second one a fresh directory.
	m := buildIn(t, oneNoisy("SECOND-RUN"))
	m.StartPicked()
	waitFor(t, "the service", func() bool { return stateOf(m, "noisy").State == supervisor.Running })
	showLog(t, m)

	var out string
	waitFor(t, "the record of the last run", func() bool {
		out = paint(t, m, 70, 12).text()
		return strings.Contains(out, "SAID-BEFORE-QUITTING")
	})
	if !strings.Contains(out, "SECOND-RUN") {
		t.Errorf("the new run is not showing under the old one:\n%s", out)
	}
}

// Stopping a service should not blank the thing you opened the view to read.
func TestAStoppedServiceStillShowsItsLastRun(t *testing.T) {
	m := build(t, oneNoisy("BEFORE-THE-STOP"))
	m.StartPicked()
	waitFor(t, "the service", func() bool { return stateOf(m, "noisy").State == supervisor.Running })
	showLog(t, m)
	waitFor(t, "its output", func() bool {
		return strings.Contains(paint(t, m, 70, 12).text(), "BEFORE-THE-STOP")
	})

	m.StopPicked()
	waitFor(t, "the stop", func() bool { return stateOf(m, "noisy").State == supervisor.Stopped })

	out := paint(t, m, 70, 12).text()
	if !strings.Contains(out, "BEFORE-THE-STOP") {
		t.Errorf("stopping blanked the log:\n%s", out)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("nothing says the text is not live:\n%s", out)
	}
}

// Four hundred lines brought back are only useful if they can be reached. The
// wheel is what reaches them, as it does everywhere else in the application.
func TestTheWheelReachesTheLogsHistory(t *testing.T) {
	m := build(t, map[string]any{"services": []any{
		map[string]any{"name": "chatty", "cmd": []any{
			"sh", "-c", "i=1; while [ $i -le 80 ]; do printf 'line-%03d\n' $i; i=$((i+1)); done; sleep 300"}},
	}})
	m.StartPicked()
	waitFor(t, "the service", func() bool { return stateOf(m, "chatty").State == supervisor.Running })
	showLog(t, m)
	waitFor(t, "the last line", func() bool {
		return strings.Contains(paint(t, m, 70, 12).text(), "line-080")
	})
	if strings.Contains(paint(t, m, 70, 12).text(), "line-010") {
		t.Fatal("the pane is too tall for this test; the early lines are still on screen")
	}

	// Three lines a notch, and seventy lines have gone off the top.
	for i := 0; i < 25; i++ {
		m.Mouse(uv.MouseWheelEvent{X: 10, Y: 6, Button: uv.MouseWheelUp})
	}
	if out := paint(t, m, 70, 12).text(); !strings.Contains(out, "line-010") {
		t.Errorf("the wheel did not reach the history:\n%s", out)
	}
}

// Two names that sanitise to the same thing must not share a file: one would
// silently overwrite the other's record.
func TestTwoServicesNeverShareALogFile(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	built, err := module.New("supervisor", map[string]any{"services": []any{
		map[string]any{"name": "api/v1", "cmd": []any{"sh", "-c", "printf ONE; sleep 300"}},
		map[string]any{"name": "api-v1", "cmd": []any{"sh", "-c", "printf TWO; sleep 300"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	m := built.(*supervisor.Module)
	if err := m.Init(module.Context{Wake: func() {}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Resize(70, 12); err != nil {
		t.Fatal(err)
	}
	m.StartPicked()
	waitFor(t, "both", func() bool {
		return stateOf(m, "api/v1").State == supervisor.Running &&
			stateOf(m, "api-v1").State == supervisor.Running
	})
	// Both have to have printed something, or there is nothing to write.
	paint(t, m, 70, 12)
	m.Mouse(clickAt(6, 2))
	waitFor(t, "the first", func() bool {
		return strings.Contains(paint(t, m, 70, 12).text(), "ONE")
	})
	m.Mouse(clickAt(2, 0))
	paint(t, m, 70, 12)
	m.Mouse(clickAt(6, 3))
	waitFor(t, "the second", func() bool {
		return strings.Contains(paint(t, m, 70, 12).text(), "TWO")
	})
	_ = m.Close()

	entries, err := os.ReadDir(filepath.Join(state, "claudecontrol", "services"))
	if err != nil {
		t.Fatalf("no log directory: %v", err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("%d files written, want 2: %v", len(entries), names)
	}
}

// A service may be called anything. The file it is kept in may not.
func TestALogFileNameIsSafeWhateverTheServiceIsCalled(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	built, err := module.New("supervisor", map[string]any{"services": []any{
		map[string]any{"name": "../../etc/passwd", "cmd": []any{"sh", "-c", "printf SNEAKY; sleep 300"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	m := built.(*supervisor.Module)
	if err := m.Init(module.Context{Wake: func() {}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Resize(70, 12); err != nil {
		t.Fatal(err)
	}
	m.StartPicked()
	waitFor(t, "the service", func() bool {
		return stateOf(m, "../../etc/passwd").State == supervisor.Running
	})
	showLog(t, m)
	waitFor(t, "its output", func() bool {
		return strings.Contains(paint(t, m, 70, 12).text(), "SNEAKY")
	})
	_ = m.Close()

	dir := filepath.Join(state, "claudecontrol", "services")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no log directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d files written, want 1", len(entries))
	}
	// A name, not a path: the sanitised version may well keep the dots, and
	// what matters is that it cannot leave the directory it was written to.
	name := entries[0].Name()
	if strings.ContainsAny(name, `/\`) {
		t.Errorf("the file is called %q, which is a path and not a name", name)
	}
	full := filepath.Join(dir, name)
	if resolved := filepath.Clean(full); filepath.Dir(resolved) != filepath.Clean(dir) {
		t.Errorf("%q resolves to %q, outside %q", name, resolved, dir)
	}
}
