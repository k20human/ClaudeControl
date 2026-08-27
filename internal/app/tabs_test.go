package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"claudecontrol/internal/indicator"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
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

// The terminal's own tab is the one place you can see when the window is
// behind something else, which is exactly when a session asking a question
// would otherwise go unnoticed. Claude Code writes such a title for itself;
// hosting it takes that away, so the application writes the summary instead.
func TestTheHostTerminalTitleFollowsTheSessions(t *testing.T) {
	const W, H = 90, 14
	runtimeDir := t.TempDir()
	bin := build(t)
	standIn, _ := notifyStandIn(t)

	var mu sync.Mutex
	var titles []string
	watch := vt.Callbacks{Title: func(s string) {
		mu.Lock()
		titles = append(titles, s)
		mu.Unlock()
	}}

	s, err := session.Start(session.Spec{
		ID:        "claudecontrol",
		Argv:      []string{bin, "-config", "testdata/one-pane.yaml"},
		Dir:       ".",
		Callbacks: watch,
		Env: []string{
			"XDG_STATE_HOME=" + t.TempDir(),
			"XDG_RUNTIME_DIR=" + runtimeDir,
			// Nothing a test starts may reach the real desktop.
			"PATH=" + standIn + string(os.PathListSeparator) + os.Getenv("PATH"),
		},
		Width:  W,
		Height: H,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	snap := func() *screen {
		g := newScreen(W, H)
		s.Term.Draw(g, uv.Rect(0, 0, W, H))
		return g
	}

	waitForRow(t, snap, paneRow0, "P>")
	socket := waitForSocket(t, runtimeDir)

	sawTitle := func(want string) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, got := range titles {
			if strings.Contains(got, want) {
				return true
			}
		}
		return false
	}
	waitTitle := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if sawTitle(want) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("the terminal was never told %q; it was told %v", want, titles)
	}

	// Quiet: the name alone, with nothing claimed about it.
	waitTitle("ClaudeControl")

	hook := func(event, payload string) {
		t.Helper()
		cmd := exec.Command(bin, "--hook", event)
		cmd.Env = append(os.Environ(), "CLAUDECONTROL_HOOK_SOCKET="+socket)
		cmd.Stdin = strings.NewReader(payload)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hook %s: %v\n%s", event, err, out)
		}
	}

	hook("Notification", `{"session_id":"pane-1-1","cwd":"/tmp"}`)
	waitTitle("1 waiting")
	if !sawTitle(indicator.Glyph(pool.StateWaiting, time.Now())) {
		t.Error("the title carries no mark for a session that is waiting")
	}
}

// The same mark above a single pane. This is the plainest of the three places
// it appears, and the one you look at while you work.
func TestThePaneTitleCarriesTheSessionsMark(t *testing.T) {
	const W, H = 90, 14
	runtimeDir := t.TempDir()
	bin := build(t)
	standIn, _ := notifyStandIn(t)

	s, err := session.Start(session.Spec{
		ID:   "claudecontrol",
		Argv: []string{bin, "-config", "testdata/one-pane.yaml"},
		Dir:  ".",
		Env: []string{
			"XDG_STATE_HOME=" + t.TempDir(),
			"XDG_RUNTIME_DIR=" + runtimeDir,
			// Nothing a test starts may reach the real desktop.
			"PATH=" + standIn + string(os.PathListSeparator) + os.Getenv("PATH"),
		},
		Width:  W,
		Height: H,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	snap := func() *screen {
		g := newScreen(W, H)
		s.Term.Draw(g, uv.Rect(0, 0, W, H))
		return g
	}

	waitForRow(t, snap, paneRow0, "P>")
	socket := waitForSocket(t, runtimeDir)

	cmd := exec.Command(bin, "--hook", "Notification")
	cmd.Env = append(os.Environ(), "CLAUDECONTROL_HOOK_SOCKET="+socket)
	cmd.Stdin = strings.NewReader(`{"session_id":"pane-1-1","cwd":"/tmp"}`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook: %v\n%s", err, out)
	}

	waitForRow(t, snap, 0, indicator.Glyph(pool.StateWaiting, time.Now()))
	if row := snap().row(0); !strings.Contains(row, "term") {
		t.Errorf("the mark replaced the name instead of joining it: %q", row)
	}
}

// Tabs are opened and closed as you work, with the two controls in the strip.
func TestTheStripOpensAndClosesTabs(t *testing.T) {
	const W, H = 90, 14
	s, snap := run(t, tabsConfig(t), W, H)
	waitForAnywhere(t, snap, "ALPHA-HERE")

	// Focus first: the first click on an unfocused pane takes the focus and
	// goes no further.
	click(t, s, W-10, 5)
	before := snap().row(0)

	plus := columnOf(before, "+")
	if plus < 0 {
		t.Fatalf("no + in the strip: %q", before)
	}
	click(t, s, plus, 0)

	// A tab arrives. It is named after whatever it holds — a session takes
	// the name of the directory it runs in — so what is checked is that the
	// strip grew and that the two that were there are still there.
	var grown string
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		grown = snap().row(0)
		if len(grown) > len(before) && strings.Contains(grown, "×") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(grown) <= len(before) {
		t.Fatalf("the strip did not grow: %q then %q", before, grown)
	}
	for _, keep := range []string{"alpha", "beta"} {
		if !strings.Contains(grown, keep) {
			t.Errorf("opening a tab lost %q: %q", keep, grown)
		}
	}

	// The cross is on the tab in front of you and no other, so a stray click
	// cannot close something you were not reading.
	cross := columnOf(grown, "×")
	if cross < 0 {
		t.Fatalf("no × in the strip: %q", grown)
	}
	click(t, s, cross, 0)

	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if row := snap().row(0); len(row) <= len(before) {
			// And what remains is what was there first.
			if !strings.Contains(row, "alpha") || !strings.Contains(row, "beta") {
				t.Errorf("closing a tab took the others with it: %q", row)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the strip did not shrink after closing: %q", snap().row(0))
}
