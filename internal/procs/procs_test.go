package procs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/procs"
)

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

// A service already running is found by what it is running and where. The
// directory alone would not do: a project can run two different scripts at
// once, which is exactly what portal does with vite and nodemon.
func TestFindMatchesTheCommandAndTheDirectory(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()

	argv := []string{"sh", "-c", "sleep 30"}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	var found []procs.Proc
	waitFor(t, "the process", func() bool {
		got, err := procs.Find(dir, argv)
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		found = got
		return len(got) == 1
	})
	if found[0].Pid != cmd.Process.Pid {
		t.Errorf("found pid %d, want %d", found[0].Pid, cmd.Process.Pid)
	}

	// Same command, elsewhere.
	if got, _ := procs.Find(other, argv); len(got) != 0 {
		t.Errorf("a process in another directory matched: %v", got)
	}
	// Same directory, another command.
	if got, _ := procs.Find(dir, []string{"sh", "-c", "sleep 31"}); len(got) != 0 {
		t.Errorf("another command matched: %v", got)
	}
}

// A pid is reused eventually. Alive has to mean "still that process", not
// "something answers to that number" — signalling a recycled pid would kill
// something unrelated.
func TestAliveNoticesAProcessThatWasReplaced(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30")
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	p, err := procs.Read(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !p.Alive() {
		t.Fatal("a running process is not alive")
	}

	// A different start time is a different process, whatever the number says.
	imposter := p
	imposter.Start++
	if imposter.Alive() {
		t.Error("a process with another start time was taken for this one")
	}

	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	waitFor(t, "the process to go", func() bool { return !p.Alive() })
}

// A launcher's children have to go with it, and the process group must not be
// touched: a service adopted from a shell shares that shell's group, and
// signalling it wholesale would kill the terminal the person is working in.
func TestTerminateTakesTheSubtreeAndNotTheGroup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child.pid")
	cmd := exec.Command("sh", "-c", "sleep 300 & echo $! > "+marker+"; wait")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	// Started without Setsid, so it sits in the test binary's own group —
	// exactly the situation an adopted service is in.
	p, err := procs.Read(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if p.Owned() {
		t.Fatalf("the process leads its own group; this test needs one that does not")
	}

	var child int
	waitFor(t, "the child's pid", func() bool {
		raw, err := os.ReadFile(marker)
		if err != nil {
			return false
		}
		child = atoi(strings.TrimSpace(string(raw)))
		return child > 0
	})

	if err := p.Terminate(2 * time.Second); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	waitFor(t, "the child to go", func() bool {
		_, err := os.Stat(filepath.Join("/proc", itoa(child)))
		return err != nil
	})
	// And this test process, which shares the group, is obviously still here.
	if _, err := os.Stat("/proc/self"); err != nil {
		t.Fatal("the test itself was signalled")
	}
}

// Owned is what decides how a service can be stopped, so it has to be right
// about the two cases that exist.
func TestOwnedDistinguishesAGroupLeader(t *testing.T) {
	p := procs.Proc{Pid: 42, PGID: 42}
	if !p.Owned() {
		t.Error("a group leader is not reported as owned")
	}
	p.PGID = 7
	if p.Owned() {
		t.Error("a process in someone else's group is reported as owned")
	}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// A process that has ended but has not been collected by its parent still has
// a directory in /proc. Counting it as running would make a stop appear to
// hang for the whole grace period, and signalling it achieves nothing.
func TestAZombieIsNotAlive(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Deliberately not waiting: that is what leaves a zombie.
	pid := cmd.Process.Pid
	p, err := procs.Read(pid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	waitFor(t, "the process to end", func() bool {
		got, err := procs.Read(pid)
		return err == nil && got.State == 'Z'
	})
	if p.Alive() {
		t.Error("a zombie is reported as alive")
	}
	if _, err := os.Stat(filepath.Join("/proc", itoa(pid))); err != nil {
		t.Skip("the zombie was collected before the check; nothing to prove")
	}
	_ = cmd.Wait()
}

// A program that rewrites its own process title reports one argument with
// spaces in it rather than the arguments it was started with. npm does — a
// service configured as [npm, run, dev] appears in /proc as the single string
// "npm run dev" — and a supervisor that compared them element by element could
// never recognise its own service as already running. It would then start a
// second copy, and two servers would fight over one port.
func TestArgvMatchesAProcessThatRewroteItsTitle(t *testing.T) {
	for _, c := range []struct {
		name       string
		got, want  []string
		shouldPass bool
	}{
		{"as started", []string{"npm", "run", "dev"}, []string{"npm", "run", "dev"}, true},
		{"title rewritten", []string{"npm run dev"}, []string{"npm", "run", "dev"}, true},
		{"the other way round", []string{"npm", "run", "dev"}, []string{"npm run dev"}, true},
		{"a different script", []string{"npm run build"}, []string{"npm", "run", "dev"}, false},
		{"a different program", []string{"yarn run dev"}, []string{"npm", "run", "dev"}, false},
		{"nothing at all", nil, []string{"npm", "run", "dev"}, false},
	} {
		if got := procs.SameArgv(c.got, c.want); got != c.shouldPass {
			t.Errorf("%s: SameArgv(%v, %v) = %v", c.name, c.got, c.want, got)
		}
	}
}
