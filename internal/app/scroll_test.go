package app_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wheelAt turns the wheel the way a terminal reports it: buttons 64 and 65 in
// the SGR encoding.
func wheelAt(t *testing.T, s sender, x, y int, up bool) {
	t.Helper()
	b := 65
	if up {
		b = 64
	}
	s.SendText(fmt.Sprintf("\x1b[<%d;%d;%dM", b, x+1, y+1))
	time.Sleep(120 * time.Millisecond)
}

// historyConfig prints more numbered lines than the pane can hold, so most of
// them are in the scrollback rather than on screen.
func historyConfig(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "h.yaml")
	if err := os.WriteFile(cfg, []byte(`layout:
  module: term
  options:
    cmd: [sh, -c, "i=1; while [ $i -le 60 ]; do printf 'mark-%03d\\n' $i; i=$((i+1)); done; cat"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// A guest on the normal screen does not scroll: it prints, and what it printed
// goes into the scrollback. In a terminal you reach that with the terminal's
// own scrollbar — hosting the guest takes that away, so the wheel reaches it
// here instead.
func TestTheWheelReachesTheHistory(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, historyConfig(t), W, H)
	waitForAnywhere(t, snap, "mark-060")

	if anywhere(snap(), "mark-030") {
		t.Fatalf("the pane is too tall for this test; mark-030 is still on screen")
	}

	click(t, s, 10, 5)
	for i := 0; i < 8; i++ {
		wheelAt(t, s, 10, 5, true)
	}
	waitForAnywhere(t, snap, "mark-030")

	// The title says how far back you are, which is what explains why typing
	// seems to do nothing where you are looking.
	if row := snap().row(0); !strings.Contains(row, "↑") {
		t.Errorf("the title does not say the view is scrolled back: %q", row)
	}

	// And down again returns to the newest.
	for i := 0; i < 12; i++ {
		wheelAt(t, s, 10, 5, false)
	}
	waitForAnywhere(t, snap, "mark-060")
	if row := snap().row(0); strings.Contains(row, "↑") {
		t.Errorf("the title still says scrolled back at the bottom: %q", row)
	}
}

// Typing returns to the bottom, the way a terminal jumps back to the prompt:
// what you type appears where the cursor is, and you should be looking at it.
func TestTypingReturnsToTheBottom(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, historyConfig(t), W, H)
	waitForAnywhere(t, snap, "mark-060")

	click(t, s, 10, 5)
	for i := 0; i < 8; i++ {
		wheelAt(t, s, 10, 5, true)
	}
	waitForAnywhere(t, snap, "mark-030")

	s.SendText("Z")
	waitForAnywhere(t, snap, "mark-060")
	if anywhere(snap(), "mark-030") {
		t.Error("typing did not return to the bottom")
	}
}

// A guest that has taken the whole screen is drawing its own view and has no
// history behind it — nothing has scrolled off. The wheel is its own.
func TestAFullScreenGuestKeepsTheWheel(t *testing.T) {
	const W, H = 60, 12
	cfg := filepath.Join(t.TempDir(), "alt.yaml")
	// Enters the alternate screen, asks for the mouse, then echoes what it is
	// sent so that anything forwarded is visible.
	if err := os.WriteFile(cfg, []byte("layout:\n  module: term\n  options:\n    cmd: [sh, -c, \"printf '\\x1b[?1049h\\x1b[?1000h\\x1b[?1006hREADY'; cat -v\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, snap := run(t, cfg, W, H)
	waitForAnywhere(t, snap, "READY")

	click(t, s, 10, 5)
	wheelAt(t, s, 10, 5, true)
	waitForAnywhere(t, snap, "[<64;")

	if row := snap().row(0); strings.Contains(row, "↑") {
		t.Errorf("the view scrolled although the guest owns the wheel: %q", row)
	}
}
