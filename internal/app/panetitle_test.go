package app_test

import (
	"fmt"
	"testing"
	"time"
)

// titleBgAt describes the background of a title cell. The exact colour does
// not matter — what matters is that the focused pane's differs from the rest
// and that the difference follows the focus.
func titleBgAt(g *screen, x int) string {
	c := g.CellAt(x, 0)
	if c == nil {
		return "<nothing>"
	}
	return fmt.Sprint(c.Style.Bg)
}

func TestThePaneTitleFollowsTheFocus(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")
	waitForRow(t, snap, paneRow0, "R>")
	// Each title names its module.
	waitForRow(t, snap, 0, "term")

	const inLeft, inRight = 1, W - 2
	g := snap()
	left, right := titleBgAt(g, inLeft), titleBgAt(g, inRight)
	if left == right {
		t.Fatalf("both titles are %s; the focused pane is not marked", left)
	}

	// The right pane starts at column 30.
	click(t, s, 45, 5)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		g = snap()
		if titleBgAt(g, inLeft) == right && titleBgAt(g, inRight) == left {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("after focusing the right pane the titles are %s and %s, want them swapped",
		titleBgAt(snap(), inLeft), titleBgAt(snap(), inRight))
}

// A click on the title row must not reach the guest: the guests here echo
// everything they are sent, so anything forwarded would show up as text.
func TestClickingATitleDoesNotReachTheGuest(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")
	waitForRow(t, snap, paneRow0, "R>")
	quiet := snap().row(paneRow0)

	click(t, s, 5, 0)
	time.Sleep(300 * time.Millisecond)
	if row := snap().row(paneRow0); row != quiet {
		t.Errorf("the first content row went from %q to %q — the click reached the guest", quiet, row)
	}
}
