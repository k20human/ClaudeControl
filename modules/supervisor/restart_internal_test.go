package supervisor

import (
	"os"
	"path/filepath"
	"testing"

	"claudecontrol/internal/relay"
)

// relaysFor is how many relays are serving a service's directory. More than
// one is the failure: two relays are two servers on one port.
func relaysFor(m *Module, name string) []int {
	var out []int
	for _, p := range liveRelays(m.relayDir(name)) {
		out = append(out, p.Pid)
	}
	return out
}

// Restarting a kept service stops it first.
//
// It did not. A kept service is shown through a session attached to a log —
// a session holding no process at all — and the restart terminated that
// session, which ends nothing. The service stayed up, a second relay was
// started beside it, and the new server found its port taken. Five relays
// from four different days were found running this way.
func TestRestartingAKeptServiceStopsItFirst(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	bin := buildBinary(t)

	m := keptModule(t, bin, "restartable", "sleep 300", t.TempDir())
	m.StartPicked()
	until(t, "the service", func() bool { return running(m, "restartable").State == Running })

	before := relaysFor(m, "restartable")
	if len(before) != 1 {
		t.Fatalf("%d relays before the restart, want 1", len(before))
	}
	firstPid := running(m, "restartable").Pid

	m.RestartPicked()
	until(t, "the service to come back", func() bool {
		s := running(m, "restartable")
		return s.State == Running && s.Pid > 0 && s.Pid != firstPid
	})

	after := relaysFor(m, "restartable")
	if len(after) != 1 {
		t.Fatalf("%d relays after the restart, want 1: %v", len(after), after)
	}
	if after[0] == before[0] {
		t.Errorf("the relay was never replaced (%d): the restart started nothing new", after[0])
	}
	if aliveNow(firstPid) {
		t.Errorf("the service the restart was meant to stop (%d) is still running", firstPid)
	}
}

// Starting a service a relay is already serving takes that relay over.
//
// The record a relay writes is overwritten by the next start, so a relay from
// an earlier run of this application leaves nothing in the file to find it by.
// The process table still names it, and that is what has to be asked — or a
// second relay goes up beside the first, which is what happened for days.
func TestAStartTakesOverARelayTheRecordForgot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	bin := buildBinary(t)

	m := keptModule(t, bin, "forgotten", "sleep 300", t.TempDir())
	m.StartPicked()
	until(t, "the service", func() bool { return running(m, "forgotten").State == Running })

	dir := m.relayDir("forgotten")
	before := relaysFor(m, "forgotten")
	if len(before) != 1 {
		t.Fatalf("%d relays to begin with, want 1", len(before))
	}

	// What a later start used to do to the record, and what a crash does to
	// it: the relay is still there, and nothing in the file says so.
	if err := os.Remove(filepath.Join(dir, relay.StateFile)); err != nil {
		t.Fatalf("removing the record: %v", err)
	}
	if _, ok := relay.ReadState(dir); ok {
		t.Fatal("the record is still readable; the test proves nothing")
	}

	m.StopPicked() // forget it here too, the way a new run of the application has
	m.StartPicked()

	after := relaysFor(m, "forgotten")
	if len(after) != 1 {
		t.Fatalf("%d relays after starting again, want 1: %v", len(after), after)
	}
}
