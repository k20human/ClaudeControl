package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tabsConfig is a pane of tabs beside a shell, each tab printing its own
// marker so the one on screen is unmistakable.
func tabsConfig(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "tabs.yaml")
	if err := os.WriteFile(cfg, []byte(`layout:
  split: horizontal
  ratios: [1, 1]
  children:
    - module: term
      options: { cmd: [sh, -c, "printf 'L>'; cat"] }
    - module: tabs
      options:
        tabs:
          - { title: alpha, module: term, options: { cmd: [sh, -c, "printf ALPHA-HERE; cat"] } }
          - { title: beta,  module: term, options: { cmd: [sh, -c, "printf BETA-HERE; cat"] } }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The strip is drawn on the row the pane title would have taken, and only the
// tab on screen is drawn.
func TestATabbedPaneShowsOneTabAtATime(t *testing.T) {
	const W, H = 90, 14
	_, snap := run(t, tabsConfig(t), W, H)
	waitForAnywhere(t, snap, "ALPHA-HERE")

	g := snap()
	if !strings.Contains(g.row(0), "alpha") || !strings.Contains(g.row(0), "beta") {
		t.Errorf("the strip is not on the first row: %q", g.row(0))
	}
	if anywhere(g, "BETA-HERE") {
		t.Errorf("both tabs were drawn:\n%s", g.row(paneRow0))
	}
}

// The shortcut has to be one the decoder reports as a key. The obvious
// candidates are not: ESC = is DECKPAM and ESC [ is CSI, so a decoder reads
// them as sequences and the key never arrives. This is the test that catches
// that, by asking for the other tab and looking at what is on screen.
func TestTheTabShortcutActuallyReachesTheApplication(t *testing.T) {
	const W, H = 90, 14
	s, snap := run(t, tabsConfig(t), W, H)
	waitForAnywhere(t, snap, "ALPHA-HERE")

	// The tabbed pane has to be focused for the shortcut to mean anything.
	click(t, s, W-10, 5)
	s.SendText("\x1b'") // alt+'
	waitForAnywhere(t, snap, "BETA-HERE")
	if anywhere(snap(), "ALPHA-HERE") {
		t.Error("the first tab is still drawn after switching")
	}

	s.SendText("\x1b;") // alt+;
	waitForAnywhere(t, snap, "ALPHA-HERE")
}

// Clicking a label switches to it, because the strip is what you look at.
//
// The pane has to be focused first. That is the application's rule everywhere:
// the first click on an unfocused pane takes the focus and is swallowed, so
// that moving to a pane can never trigger something inside it. A tab strip is
// chrome rather than guest content, but the application cannot tell them
// apart — the strip is drawn by the module, and what the application sees is a
// click on a pane.
func TestClickingATabSwitchesToIt(t *testing.T) {
	const W, H = 90, 14
	s, snap := run(t, tabsConfig(t), W, H)
	waitForAnywhere(t, snap, "ALPHA-HERE")

	click(t, s, W-10, 5)

	row := snap().row(0)
	at := columnOf(row, "beta")
	if at < 0 {
		t.Fatalf("no beta label: %q", row)
	}
	click(t, s, at, 0)
	waitForAnywhere(t, snap, "BETA-HERE")
}

// Typing reaches the tab on screen, and only it. A pane of tabs that sent
// keystrokes to a hidden session would be worse than no tabs at all.
func TestTypingReachesTheTabOnScreen(t *testing.T) {
	const W, H = 90, 14
	s, snap := run(t, tabsConfig(t), W, H)
	waitForAnywhere(t, snap, "ALPHA-HERE")

	click(t, s, W-10, 5)
	s.SendText("Z")
	waitForAnywhere(t, snap, "ALPHA-HEREZ")

	s.SendText("\x1b'")
	waitForAnywhere(t, snap, "BETA-HERE")
	s.SendText("Y")
	waitForAnywhere(t, snap, "BETA-HEREY")

	// And the first tab kept its own keystroke rather than the second's.
	s.SendText("\x1b;")
	waitForAnywhere(t, snap, "ALPHA-HEREZ")
	if anywhere(snap(), "ALPHA-HEREZY") {
		t.Error("a keystroke reached the tab that was not on screen")
	}
	time.Sleep(50 * time.Millisecond)
}
