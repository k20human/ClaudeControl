package app_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/clipboard"
)

// press, moveTo and release drive a drag the way a terminal reports one.
func press(t *testing.T, s sender, x, y int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1))
	time.Sleep(80 * time.Millisecond)
}

func selectConfig(t *testing.T, script string) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "s.yaml")
	if err := os.WriteFile(cfg, []byte("layout:\n  module: term\n  options:\n    cmd: [sh, -c, "+script+"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// reversedAt reports whether a cell is drawn inverted, which is how a
// selection is shown.
func reversedAt(g *screen, x, y int) bool {
	return reversed(g, x, y)
}

// A guest that asked only for button events cannot see a drag: it arrives as a
// press and a release with nothing in between. The gesture is therefore free,
// and it is what a person expects to select text with.
func TestDraggingSelectsTextAGuestCannotUse(t *testing.T) {
	const W, H = 60, 10
	s, snap := run(t, selectConfig(t, `"printf 'HELLO-WORLD'; printf '\x1b[?1000h\x1b[?1006h'; cat -v"`), W, H)
	waitForAnywhere(t, snap, "HELLO-WORLD")

	row := paneRow0
	from := columnOf(snap().row(row), "HELLO")
	if from < 0 {
		t.Fatalf("no text to select: %q", snap().row(row))
	}

	click(t, s, 5, row) // focus first
	press(t, s, from, row)
	drag(t, s, from+4, row)
	release(t, s, from+4, row)

	g := snap()
	for x := from; x <= from+4; x++ {
		if !reversedAt(g, x, row) {
			t.Errorf("column %d is not marked as selected: %q", x, g.row(row))
		}
	}
	if reversedAt(g, from+6, row) {
		t.Errorf("the selection ran past where the drag ended: %q", g.row(row))
	}
}

// The guest still gets its clicks: a press that never moves is a click, and a
// guest that asked for button events is entitled to it.
func TestAClickStillReachesTheGuest(t *testing.T) {
	const W, H = 60, 10
	s, snap := run(t, selectConfig(t, `"printf 'READY'; printf '\x1b[?1000h\x1b[?1006h'; cat -v"`), W, H)
	waitForAnywhere(t, snap, "READY")

	click(t, s, 5, paneRow0)  // focus
	click(t, s, 10, paneRow0) // and a real click, which the guest echoes
	waitForAnywhere(t, snap, "[<0;11;")
}

// A guest that does follow the pointer keeps its drag: it is doing something
// with it, and taking the gesture would break whatever that is.
func TestAGuestThatFollowsThePointerKeepsItsDrag(t *testing.T) {
	const W, H = 60, 10
	s, snap := run(t, selectConfig(t, `"printf 'READY'; printf '\x1b[?1002h\x1b[?1006h'; cat -v"`), W, H)
	waitForAnywhere(t, snap, "READY")

	click(t, s, 5, paneRow0)
	press(t, s, 10, paneRow0)
	drag(t, s, 20, paneRow0)
	release(t, s, 20, paneRow0)

	// The motion reached it, and nothing was selected here.
	waitForAnywhere(t, snap, "[<32;21;")
	g := snap()
	for x := 10; x <= 20; x++ {
		if reversedAt(g, x, paneRow0) {
			t.Fatalf("a pane whose guest owns the drag showed a selection: %q", g.row(paneRow0))
		}
	}
}

// Copying without a selection says so rather than doing nothing.
func TestCopyingNothingSaysSo(t *testing.T) {
	const W, H = 60, 10
	s, snap := run(t, selectConfig(t, `"printf 'READY'; cat"`), W, H)
	waitForAnywhere(t, snap, "READY")

	rightClick(t, s, 10, paneRow0)
	waitForAnywhere(t, snap, "copy")

	row := snap().row(0)
	at := columnOf(row, "copy")
	if at < 0 {
		// The menu may be drawn lower; find it wherever it is.
		for y := 0; y < H; y++ {
			if c := columnOf(snap().row(y), "copy"); c >= 0 {
				click(t, s, c, y)
				at = c
				break
			}
		}
	} else {
		click(t, s, at, 0)
	}
	if at < 0 {
		t.Fatal("no copy entry in the menu")
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(snap().row(H-1), "nothing is selected") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("copying nothing said nothing: %q", snap().row(H-1))
}

// A copy that reached nothing must not read like a copy that worked. Without a
// clipboard tool the terminal is asked directly, and most refuse — so the
// menu says what it will need before you press it, and the message afterwards
// says what happened rather than what was attempted.
func TestCopyWithoutAClipboardToolSaysSoBeforeAndAfter(t *testing.T) {
	const W, H = 70, 10
	s, snap := run(t, selectConfig(t, `"printf 'HELLO-WORLD'; cat"`), W, H)
	waitForAnywhere(t, snap, "HELLO-WORLD")

	// An empty PATH for the hosted application would break everything else it
	// runs, so this checks the two messages against whatever this machine has.
	_, haveHelper := clipboardHelper()

	row := paneRow0
	from := columnOf(snap().row(row), "HELLO")
	click(t, s, 5, row)
	press(t, s, from, row)
	drag(t, s, from+4, row)
	release(t, s, from+4, row)

	rightClick(t, s, 10, row)
	waitForAnywhere(t, snap, "copy")

	// The entry warns beforehand, exactly when there is something to warn
	// about.
	warned := anywhere(snap(), "no clipboard tool")
	if warned == haveHelper {
		t.Errorf("the menu warns=%v while a helper is present=%v", warned, haveHelper)
	}

	var at, atY = -1, -1
	for y := 0; y < H; y++ {
		if c := columnOf(snap().row(y), "copy"); c >= 0 {
			at, atY = c, y
			break
		}
	}
	if at < 0 {
		t.Fatal("no copy entry in the menu")
	}
	click(t, s, at, atY)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		bar := snap().row(H - 1)
		switch {
		case haveHelper && strings.Contains(bar, "copied"):
			return
		case !haveHelper && strings.Contains(bar, "install wl-clipboard"):
			// And it must not claim to have done anything.
			if strings.Contains(bar, "copied") {
				t.Errorf("a copy that reached nothing read as a success: %q", bar)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("copying said nothing useful: %q", snap().row(H-1))
}

// clipboardHelper reports whether this machine has one, the same way the
// application decides.
func clipboardHelper() (string, bool) { return clipboard.Helper() }
