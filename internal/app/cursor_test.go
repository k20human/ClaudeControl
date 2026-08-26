package app_test

import (
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// cursorWatch counts the show/hide sequences reaching the host terminal.
// It has to be installed through session.Spec.Configure rather than on the
// running session: SetCallbacks is promoted from the unguarded emulator, so
// calling it while the pump is writing is a race — which the detector duly
// found when this was first written the obvious way.
func cursorWatch() (func(vt.Terminal), func() int) {
	var mu sync.Mutex
	n := 0
	install := func(term vt.Terminal) {
		e, ok := term.(*vt.SafeEmulator)
		if !ok {
			return
		}
		e.SetCallbacks(vt.Callbacks{CursorVisibility: func(bool) {
			mu.Lock()
			n++
			mu.Unlock()
		}})
	}
	return install, func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// The hologram animates without pause, so the application redraws without
// pause. Toggling the cursor once per frame costs two escape sequences every
// frame — at sixty frames a second the real terminal never holds the cursor
// still long enough to show it, which is why it looked absent. Nothing about
// the cursor changes between those frames, so nothing should be sent.
func TestAnAnimatedPaneDoesNotFlickerTheCursor(t *testing.T) {
	const W, H = 80, 16
	install, count := cursorWatch()
	s, snap := runWith(t, "testdata/holo-term.yaml", W, H, install)

	waitForRow(t, snap, paneRow0, "L>")
	s.SendText("Z")
	waitForRow(t, snap, paneRow0, "L>Z")
	// Let the frame after the keystroke settle before counting.
	time.Sleep(500 * time.Millisecond)

	from := count()
	time.Sleep(2 * time.Second)
	if got := count() - from; got != 0 {
		t.Errorf("%d cursor changes in two idle seconds, want none", got)
	}
}

// Where the cursor ends up is not cosmetic: the terminal draws it, so a cursor
// left in another pane is a stray block on someone else's content. The
// renderer moves the hardware cursor as it paints, so the application has to
// have the last word.
func TestTheCursorSitsWhereYouType(t *testing.T) {
	const W, H = 80, 16
	s, snap := run(t, "testdata/holo-term.yaml", W, H)

	waitForRow(t, snap, paneRow0, "L>")
	s.SendText("Z")
	waitForRow(t, snap, paneRow0, "L>Z")

	// The left pane starts at column 0, so the guest's column is the screen's.
	// "L>Z" is three cells, and the cursor follows them.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if reversed(snap(), 3, paneRow0) {
			// And nowhere else: a cursor left in another pane is a block
			// sitting on someone else's content.
			for x := 4; x < W; x++ {
				if reversed(snap(), x, paneRow0) {
					t.Fatalf("column %d of the first content row is reversed too", x)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("no cursor at column 3 of the first content row; row 0 = %q", snap().row(paneRow0))
}

// reversed reports whether the cell carries the reverse attribute.
func reversed(g *screen, x, y int) bool {
	c := g.CellAt(x, y)
	return c != nil && c.Style.Attrs&uv.AttrReverse != 0
}
