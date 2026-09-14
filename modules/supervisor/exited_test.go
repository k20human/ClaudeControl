package supervisor_test

import (
	"strings"
	"syscall"
	"testing"

	"claudecontrol/modules/supervisor"
)

// A service that ended and is running again — started by hand in a terminal,
// say — must be noticed. It could not be: the scan skipped anything holding a
// session, and a service that ends keeps its session so its output stays
// readable. The row then claimed "exited" for as long as the pane was open,
// and no amount of scanning could correct it.
func TestAnExitedServiceIsAdoptedWhenItIsRunningAgain(t *testing.T) {
	dir := t.TempDir()
	m := build(t, map[string]any{"services": []any{
		map[string]any{"name": "ghost", "cmd": []any{"sh", "-c", "sleep 300"}, "dir": dir},
	}})

	m.StartPicked()
	waitFor(t, "the service", func() bool { return stateOf(m, "ghost").State == supervisor.Running })

	// Killed from outside, the way a service dies when something else ends it.
	pid := stateOf(m, "ghost").Pid
	_ = syscall.Kill(pid, syscall.SIGKILL)
	waitFor(t, "the exit", func() bool { return stateOf(m, "ghost").State == supervisor.Exited })

	// And now the same command is running again, started elsewhere.
	outside := outsideService(t, dir, "sleep 300")
	waitFor(t, "the adoption", func() bool {
		m.Scan()
		got := stateOf(m, "ghost")
		return got.State == supervisor.Running && got.Adopted && got.Pid == outside.Pid
	})
}

// 143 is what a shell and npm report for a process ended by SIGTERM. Printed
// as a number it reads like a failure; named, it says that something stopped
// the service rather than that the service broke.
func TestASignalExitIsNamedRatherThanNumbered(t *testing.T) {
	m := build(t, map[string]any{"services": []any{
		map[string]any{"name": "signalled", "cmd": []any{"sh", "-c", "exit 143"}},
		map[string]any{"name": "failed", "cmd": []any{"sh", "-c", "exit 2"}},
	}})
	m.StartPicked()
	waitFor(t, "both to end", func() bool {
		return stateOf(m, "signalled").State == supervisor.Exited &&
			stateOf(m, "failed").State == supervisor.Exited
	})

	out := paint(t, m, 80, 8).text()
	if !strings.Contains(out, "SIGTERM") {
		t.Errorf("143 is not named as a signal:\n%s", out)
	}
	if !strings.Contains(out, "code 2") {
		t.Errorf("an ordinary failure lost its code:\n%s", out)
	}
	if strings.Contains(out, "code 143") {
		t.Errorf("143 is still shown as a number:\n%s", out)
	}
}

// A command that could not be run at all says so.
//
// 127 is what a shell reports for a command it could not find, and npm passes
// it through: `npm run dev` on a project whose vite is not installed ends
// exactly this way. The line explaining it scrolls out of the log; the number
// stays in the row, and a number is not an explanation.
func TestACommandThatCouldNotRunSaysSo(t *testing.T) {
	m := build(t, map[string]any{"services": []any{
		map[string]any{"name": "missing", "cmd": []any{"sh", "-c", "exit 127"}},
		map[string]any{"name": "unreadable", "cmd": []any{"sh", "-c", "exit 126"}},
		map[string]any{"name": "failed", "cmd": []any{"sh", "-c", "exit 2"}},
	}})
	m.StartPicked()
	waitFor(t, "all three to end", func() bool {
		for _, name := range []string{"missing", "unreadable", "failed"} {
			if stateOf(m, name).State != supervisor.Exited {
				return false
			}
		}
		return true
	})

	out := paint(t, m, 90, 10).text()
	if !strings.Contains(out, "no such command") {
		t.Errorf("127 is not named:\n%s", out)
	}
	if !strings.Contains(out, "not executable") {
		t.Errorf("126 is not named:\n%s", out)
	}
	// And an ordinary failure keeps its number: naming one this application
	// cannot explain would be inventing a reason.
	if !strings.Contains(out, "code 2") {
		t.Errorf("an ordinary failure lost its code:\n%s", out)
	}
	if strings.Contains(out, "code 127") || strings.Contains(out, "code 126") {
		t.Errorf("a code that has a name is still shown as a number:\n%s", out)
	}
}

// `npm run dev` is a launcher. Kill it and the server it started can keep its
// port, which is the state that makes a row reading "exited" so misleading:
// true of the process, false of the service.
//
// nohup is what makes the child outlive its parent here. A plain background
// job would not: the service leads its own terminal session, and the kernel
// hangs up on that session's foreground group when its leader dies. A server
// that survives its launcher has escaped that one way or another, and nohup is
// the shortest way to arrange it on purpose.
func TestAnExitThatLeftSomethingRunningSaysSo(t *testing.T) {
	dir := t.TempDir()
	m := build(t, map[string]any{"services": []any{
		map[string]any{"name": "launcher", "cmd": []any{
			"sh", "-c", "nohup sleep 300 >/dev/null 2>&1 & sleep 30"}, "dir": dir},
	}})
	m.StartPicked()
	waitFor(t, "the service", func() bool { return stateOf(m, "launcher").State == supervisor.Running })

	// The scan is what learns the shape of a service's process tree.
	waitFor(t, "its child", func() bool {
		m.Scan()
		return stateOf(m, "launcher").Children > 0
	})

	// The launcher dies; what it started does not.
	_ = syscall.Kill(stateOf(m, "launcher").Pid, syscall.SIGKILL)
	waitFor(t, "the exit", func() bool { return stateOf(m, "launcher").State == supervisor.Exited })

	if left := stateOf(m, "launcher").Left; left < 1 {
		t.Fatalf("Left = %d; the backgrounded sleep is still running", left)
	}
	// The survivor count sits before the time: a narrow pane clips from the
	// right, and of the three facts on that row the time is the one you can
	// most afford to lose.
	out := paint(t, m, 80, 8).text()
	if !strings.Contains(out, "1 left") {
		t.Errorf("the row does not say what survived:\n%s", out)
	}
	if strings.Index(out, "1 left") > strings.Index(out, " ago") {
		t.Errorf("the time comes before the survivors, so a narrow pane loses them:\n%s", out)
	}
}
