package relay_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"claudecontrol/internal/relay"
)

// binary builds the application, which is also the relay.
func binary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claudecontrol")
	cmd := exec.Command("go", "build", "-o", bin, "claudecontrol/cmd/claudecontrol")
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// start launches a relay in its own session, the way the supervisor does, and
// returns its directory.
func start(t *testing.T, bin, dir string, argv ...string) *exec.Cmd {
	t.Helper()
	raw, _ := json.Marshal(relay.State{Dir: dir})
	if err := os.WriteFile(filepath.Join(dir, relay.StateFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--relay", dir}, argv...)
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start relay: %v", err)
	}
	t.Cleanup(func() {
		if st, ok := relay.ReadState(dir); ok && st.Pid > 0 {
			_ = syscall.Kill(-st.Pid, syscall.SIGKILL)
		}
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The whole reason the relay exists: a service that keeps printing must keep
// running, however long nobody reads it. Left on a terminal nobody drains it
// does not die — it blocks on a full buffer, holding its port and answering
// nothing, which is worse than having been stopped.
func TestAChattyServiceNeverBlocks(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")

	// It prints a great deal and records its progress somewhere the terminal
	// cannot reach, so that a block shows as a number that stops moving.
	script := "i=0; while true; do i=$((i+1)); " +
		"echo \"line $i padded out so the buffer fills in seconds not minutes\"; " +
		"echo $i > " + counter + "; done"
	start(t, bin, dir, "sh", "-c", script)

	waitFor(t, "the service to start", func() bool {
		st, ok := relay.ReadState(dir)
		return ok && st.Pid > 0
	})

	// Well past the seventy thousand lines that fill a pty on this machine.
	waitFor(t, "a hundred thousand lines", func() bool {
		return countOf(t, counter) > 100000
	})

	was := countOf(t, counter)
	time.Sleep(1500 * time.Millisecond)
	if now := countOf(t, counter); now <= was {
		t.Fatalf("the service stopped at %d; it is blocked on its terminal", now)
	}
}

// countOf reads the counter, retrying past the instant the writer has
// truncated it and not yet written: an empty file is a race, not a service
// that has gone back to zero.
func countOf(t *testing.T, path string) int {
	t.Helper()
	for i := 0; i < 20; i++ {
		raw, err := os.ReadFile(path)
		if err == nil {
			if n, cerr := strconv.Atoi(strings.TrimSpace(string(raw))); cerr == nil && n > 0 {
				return n
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 0
}

// The log holds what the terminal carried, escape sequences and all. That is
// what lets the colours come back: the file is replayed through an emulator
// rather than read as text.
func TestTheLogKeepsTheEscapeSequences(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	start(t, bin, dir, "sh", "-c", "printf '\\033[31mRED-HERE\\033[0m\\n'; sleep 30")

	waitFor(t, "the output", func() bool {
		raw, err := os.ReadFile(filepath.Join(dir, relay.LogFile))
		return err == nil && strings.Contains(string(raw), "RED-HERE")
	})
	raw, _ := os.ReadFile(filepath.Join(dir, relay.LogFile))
	if !strings.Contains(string(raw), "\033[31m") {
		t.Errorf("the colour was stripped:\n%q", string(raw))
	}
}

// The relay stands in its own session, which is what makes it outlive the
// application that asked for it: a signal sent to the application's group
// never reaches it.
//
// Killing the relay itself is a different matter and does end the service —
// the relay leads that session, and the kernel hangs up on a session whose
// leader has gone. That is why nothing ever signals it on purpose.
func TestTheRelayStandsInItsOwnSession(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	cmd := start(t, bin, dir, "sh", "-c", "sleep 60")

	waitFor(t, "the service", func() bool {
		st, ok := relay.ReadState(dir)
		return ok && st.Pid > 0
	})

	mine, theirs := sessionOf(t, os.Getpid()), sessionOf(t, cmd.Process.Pid)
	if mine == theirs {
		t.Errorf("the relay shares session %d with whoever started it", mine)
	}
}

// sessionOf is the session id from /proc, field six of stat.
func sessionOf(t *testing.T, pid int) int {
	t.Helper()
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatalf("stat %d: %v", pid, err)
	}
	text := string(raw)
	fields := strings.Fields(text[strings.LastIndex(text, ")")+1:])
	if len(fields) < 4 {
		t.Fatalf("stat %d: %q", pid, text)
	}
	sid, err := strconv.Atoi(fields[3])
	if err != nil {
		t.Fatalf("session of %d: %v", pid, err)
	}
	return sid
}

func alive(pid int) bool {
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))
	return err == nil
}

// And it says how it ended, for the run of the application that comes back to
// find it gone.
func TestTheRelayRecordsHowTheServiceEnded(t *testing.T) {
	bin := binary(t)
	dir := t.TempDir()
	start(t, bin, dir, "sh", "-c", "exit 3")

	waitFor(t, "the exit", func() bool {
		st, ok := relay.ReadState(dir)
		return ok && st.Exited
	})
	if st, _ := relay.ReadState(dir); st.Code != 3 {
		t.Errorf("Code = %d, want 3", st.Code)
	}
}
