package app_test

import (
	"strings"
	"testing"
	"time"
)

// The wheel reaches the history; finding something in it is the next thing you
// want. The shortcut has to be one the decoder reports as a key — alt+= is
// DECKPAM and alt+[ is CSI, and neither ever arrives — so this asks for the
// search and looks at what appears.
func TestTheSearchShortcutOpensTheBar(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, historyConfig(t), W, H)
	waitForAnywhere(t, snap, "mark-060")

	click(t, s, 10, 5)
	s.SendText("\x1b:") // alt+:
	waitForAnywhere(t, snap, "find")

	// Typing goes to the query, not to the guest.
	s.SendText("mark-030")
	waitForAnywhere(t, snap, "mark-030")
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if anywhere(snap(), "1/1") {
			// Escape closes it and leaves the view where the search left it.
			s.SendText("\x1b")
			time.Sleep(400 * time.Millisecond)
			if anywhere(snap(), "1/1") {
				t.Error("escape left the search bar open")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the search never counted its match:\n%s", snap().row(H-2))
}

// A query with nothing to find says so, rather than leaving you wondering
// whether it looked.
func TestASearchWithNoMatchSaysNone(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, historyConfig(t), W, H)
	waitForAnywhere(t, snap, "mark-060")

	click(t, s, 10, 5)
	s.SendText("\x1b:")
	waitForAnywhere(t, snap, "find")
	s.SendText("zzz-not-there")
	waitForAnywhere(t, snap, "none")
}

// And a button, because a search only reachable from the keyboard is a search
// half the people who want it will never find.
func TestTheSearchHasAButton(t *testing.T) {
	const W, H = 100, 12
	s, snap := run(t, historyConfig(t), W, H)
	waitForAnywhere(t, snap, "mark-060")

	bar := waitForRow(t, snap, H-1, "find").row(H - 1)
	click(t, s, columnOf(bar, "find"), H-1)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(snap().row(H-2), "find") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the button did not open the search: %q", snap().row(H-2))
}
