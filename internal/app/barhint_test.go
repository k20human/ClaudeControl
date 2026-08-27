package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/session"
)

// moveTo puts the pointer somewhere without pressing anything. SGR button 35
// is motion with no button held, which is what a terminal in motion-reporting
// mode sends as the pointer travels.
func moveTo(t *testing.T, s *session.Session, x, y int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<35;%d;%dM", x+1, y+1))
	time.Sleep(150 * time.Millisecond)
}

// An icon is legible to the person who chose it and nobody else. Resting the
// pointer on a button says what it does, and with which key.
func TestHoveringAButtonSaysWhatItDoes(t *testing.T) {
	const W, H = 120, 14
	s, snap := run(t, historyConfig(t), W, H)
	waitForAnywhere(t, snap, "mark-060")

	bar := waitForRow(t, snap, H-1, "? help").row(H - 1)
	moveTo(t, s, columnOf(bar, "? help"), H-1)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(snap().row(H-2), "every key and gesture") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if row := snap().row(H - 2); !strings.Contains(row, "every key and gesture") {
		t.Fatalf("no hint above the button: %q", row)
	}
	if row := snap().row(H - 2); !strings.Contains(row, "alt+g") {
		t.Errorf("the hint does not name the key: %q", row)
	}

	// And it gives the row back. The tip borrows a row from the pane above it,
	// so a tip that stayed would be a hole in the pane.
	moveTo(t, s, 4, 4)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !strings.Contains(snap().row(H-2), "every key and gesture") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the hint stayed after the pointer left: %q", snap().row(H-2))
}

// The tip is anchored on its button, but the last button on the bar is at the
// right-hand edge: a tip that started there would run off the screen and lose
// the words that matter most.
func TestTheHintStaysOnScreenAtTheRightEdge(t *testing.T) {
	const W, H = 120, 14
	s, snap := run(t, historyConfig(t), W, H)
	waitForAnywhere(t, snap, "mark-060")

	bar := waitForRow(t, snap, H-1, "⏻ quit").row(H - 1)
	moveTo(t, s, columnOf(bar, "⏻ quit"), H-1)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(snap().row(H-2), "alt+q") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the quit hint was cut off: %q", snap().row(H-2))
}
