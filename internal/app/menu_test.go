package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// rightClick presses and releases the right button, the way a terminal reports
// it: button 2 in the SGR encoding.
func rightClick(t *testing.T, s sender, x, y int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<2;%d;%dM", x+1, y+1))
	time.Sleep(80 * time.Millisecond)
	s.SendText(fmt.Sprintf("\x1b[<2;%d;%dm", x+1, y+1))
	time.Sleep(200 * time.Millisecond)
}

// Enabling mouse reporting takes the right button away from the terminal, and
// with it the menu the terminal would have shown. Having taken it, the
// application owes one back.
func TestRightClickOpensAMenuOnThePane(t *testing.T) {
	const W, H = 80, 16
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")

	rightClick(t, s, 6, 5)
	waitForAnywhere(t, snap, "paste")
	g := snap()
	for _, want := range []string{"paste", "new session", "close pane", "alt+n"} {
		if !anywhere(g, want) {
			t.Errorf("the menu lacks %q:\n%s", want, g.row(5))
		}
	}

	// Escape dismisses it, and the pane is itself again.
	s.SendText("\x1b")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !anywhere(snap(), "new session") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("escape left the menu open:\n%s", snap().row(5))
}

// A click elsewhere dismisses without acting, which is what every menu does.
func TestClickingAwayDismissesTheMenuWithoutActing(t *testing.T) {
	const W, H = 80, 16
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")
	before := snap().row(paneRow0)

	rightClick(t, s, 6, 5)
	waitForAnywhere(t, snap, "paste")

	// Far from the menu, inside the other pane.
	click(t, s, W-6, 10)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !anywhere(snap(), "new session") {
			// Nothing was run: the panes are as they were.
			if got := snap().row(paneRow0); got != before {
				t.Errorf("the first content row went from %q to %q", before, got)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("clicking away left the menu open")
}

// The bar was writing sentences nobody could read: ten places set a status and
// nothing ever drew it. A message now takes the bar, and the buttons it covers
// stop being clickable while it does — a button you cannot see must not be a
// button you can press.
func TestAMessageTakesTheBarAndTheButtonsWithIt(t *testing.T) {
	const W, H = 80, 16
	s, snap := run(t, "testdata/hologram.yaml", W, H)
	waitForRow(t, snap, paneRow0, "P>")
	bar := waitForRow(t, snap, H-1, "set").row(H - 1)

	// The hologram publishes settings; the terminal pane does not. Asking the
	// one that does not is the shortest way to a message.
	click(t, s, columnOf(bar, "set"), H-1)
	time.Sleep(200 * time.Millisecond)

	var got string
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		got = snap().row(H - 1)
		if strings.Contains(got, "nothing to configure") {
			// Quit stays reachable; the rest of the buttons are gone with the
			// space they occupied.
			if !strings.Contains(got, "quit") {
				t.Errorf("the way out was hidden behind a message: %q", got)
			}
			if strings.Contains(got, "+ new") {
				t.Errorf("a button was drawn over the message: %q", got)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("no message reached the bar: %q", got)
}
