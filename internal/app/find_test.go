package app_test

import (
	"testing"
	"time"

	"claudecontrol/internal/session"
)

// openPaneFind reaches the pane's own search the way you now do: right-click
// where you mean, then pick the entry.
func openPaneFind(t *testing.T, s *session.Session, snap func() *screen, x, y int) {
	t.Helper()
	rightClick(t, s, x, y)
	waitForAnywhere(t, snap, "find here")
	g := snap()
	for row := 0; row < g.h; row++ {
		if c := columnOf(g.row(row), "find here"); c >= 0 {
			click(t, s, c, row)
			return
		}
	}
	t.Fatal("the menu offered no find entry")
}

// The wheel reaches the history; finding something in it is the next thing you
// want. The shortcut has to be one the decoder reports as a key — alt+= is
// DECKPAM and alt+[ is CSI, and neither ever arrives — so this asks for the
// search and looks at what appears.
func TestTheSearchShortcutOpensTheBar(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, historyConfig(t), W, H)
	waitForAnywhere(t, snap, "mark-060")

	click(t, s, 10, 5)
	openPaneFind(t, s, snap, 10, 5)

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
	openPaneFind(t, s, snap, 10, 5)
	s.SendText("zzz-not-there")
	waitForAnywhere(t, snap, "none")
}

// The bar's button and alt+: open the search across the conversations on
// disk, which is the one you reach for from anywhere. The pane's own history
// is searched from the right-click menu, where the click already says which
// pane you mean.
func TestTheConversationSearchOpensFromBothTheBarAndTheKey(t *testing.T) {
	const W, H = 100, 16
	s, snap := run(t, historyConfig(t), W, H)
	waitForAnywhere(t, snap, "mark-060")

	bar := waitForRow(t, snap, H-1, "find").row(H - 1)
	click(t, s, columnOf(bar, "find"), H-1)
	waitForAnywhere(t, snap, "find in conversations")
	waitForAnywhere(t, snap, "type to search")

	s.SendText("\x1b") // escape closes it
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !anywhere(snap(), "find in conversations") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if anywhere(snap(), "find in conversations") {
		t.Fatal("escape left the panel open")
	}

	s.SendText("\x1b:")
	waitForAnywhere(t, snap, "find in conversations")
}
