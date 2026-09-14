package supervisor

import (
	"time"

	"claudecontrol/internal/procs"
)

// Finding the relays that are running, by asking the process table.
//
// The file a relay writes names the process it is holding, and that file is
// the only account of a service this application did not start itself. It is
// not enough to find a relay by. A start overwrites it, so a relay that
// outlived the run which wrote it leaves nothing there at all — and what it
// still owns is the port.
//
// What such a relay does leave is its own command line, which names the
// directory it serves. Nothing overwrites that, so it is what this asks.

// relayFlag is how a relay names the directory it serves, and so how one is
// recognised from outside.
const relayFlag = "--relay"

// liveRelays are the relays serving one directory. More than one is the
// failure this exists to find: two relays on a directory are two servers on a
// port.
func liveRelays(dir string) []procs.Proc {
	if dir == "" {
		return nil
	}
	all, err := procs.All()
	if err != nil {
		return nil
	}
	var out []procs.Proc
	for _, p := range all {
		// A zombie has already ended. Signalling it achieves nothing and
		// waiting for it would spend the whole grace period.
		if p.State == 'Z' {
			continue
		}
		if servesRelay(p.Cmdline, dir) {
			out = append(out, p)
		}
	}
	return out
}

// servesRelay reports whether a command line is a relay for this directory.
func servesRelay(argv []string, dir string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == relayFlag && argv[i+1] == dir {
			return true
		}
	}
	return false
}

// stopRelays ends every relay serving a directory, and everything under each
// of them.
//
// The subtree rather than the group, which is what Terminate walks: a relay
// puts itself in its own session, but what it started may have moved, and a
// service that got away is the one holding the port.
func stopRelays(dir string, grace time.Duration) {
	for _, p := range liveRelays(dir) {
		_ = p.Terminate(grace)
	}
}
