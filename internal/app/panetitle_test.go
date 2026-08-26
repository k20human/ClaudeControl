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

// A pane that is a picture wants no label above it: the hologram keeps every
// row it was given, and only its neighbour is titled.
func TestTheHologramHasNoTitleRow(t *testing.T) {
	const W, H = 80, 16
	_, snap := run(t, "testdata/holo-term.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")

	g := snap()
	if got := columnOf(g.row(0), "hologram"); got >= 0 {
		t.Errorf("the hologram pane is titled at column %d: %q", got, g.row(0))
	}
	if columnOf(g.row(0), "term") < 0 {
		t.Errorf("its neighbour lost its title: %q", g.row(0))
	}

	// And it draws into the row its neighbour gives up.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		row := []rune(snap().row(0))
		for i := W / 2; i < len(row); i++ {
			if row[i] >= '⠀' && row[i] <= '⣿' {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("nothing braille reached row 0 of the hologram pane: %q", snap().row(0))
}

// The status bar reaches the bottom of the terminal and fills its whole width.
// A last row left short would show the terminal's own background beneath the
// interface, which reads as a gap the application put there.
func TestTheStatusBarFillsTheLastRow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 18}, {64, 12}} {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			_, snap := run(t, "testdata/holo-term.yaml", w, h)
			waitForRow(t, snap, paneRow0, "L>")

			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if rowPainted(snap(), w, h-1) {
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
			g := snap()
			for x := 0; x < w; x++ {
				if c := g.CellAt(x, h-1); c == nil || c.Style.Bg == nil {
					t.Fatalf("column %d of the last row is unpainted; the row reads %q",
						x, g.row(h-1))
				}
			}
		})
	}
}

// rowPainted reports whether every cell of a row has a background of its own.
func rowPainted(g *screen, w, y int) bool {
	for x := 0; x < w; x++ {
		c := g.CellAt(x, y)
		if c == nil || c.Style.Bg == nil {
			return false
		}
	}
	return true
}
