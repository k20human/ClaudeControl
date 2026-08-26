package app_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/session"
)

// screen is a minimal uv.Screen used to snapshot what claudecontrol painted.
type screen struct {
	w, h  int
	cells []uv.Cell
}

func newScreen(w, h int) *screen {
	s := &screen{w: w, h: h, cells: make([]uv.Cell, w*h)}
	for i := range s.cells {
		s.cells[i] = uv.EmptyCell
	}
	return s
}

func (b *screen) Bounds() uv.Rectangle { return uv.Rect(0, 0, b.w, b.h) }

func (b *screen) CellAt(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return nil
	}
	return &b.cells[y*b.w+x]
}

func (b *screen) SetCell(x, y int, c *uv.Cell) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return
	}
	if c == nil {
		b.cells[y*b.w+x] = uv.EmptyCell
		return
	}
	b.cells[y*b.w+x] = *c
}

func (b *screen) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }

func (b *screen) row(y int) string {
	var sb strings.Builder
	for x := 0; x < b.w; x++ {
		sb.WriteString(b.cells[y*b.w+x].String())
	}
	return strings.TrimRight(sb.String(), " \x00")
}

// build compiles the command once per test binary run.
// paneRow0 is the screen row where a pane's own content starts. The row above
// it carries the pane title.
const paneRow0 = 1

func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claudecontrol")
	cmd := exec.Command("go", "build", "-o", bin, "claudecontrol/cmd/claudecontrol")
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

// run starts claudecontrol on its own PTY and returns a snapshot function.
//
// This is the stage-1 acceptance criterion, mechanised: the application is
// hosted exactly the way it hosts its own guests, so what the assertions read
// is what a terminal would display.
func run(t *testing.T, cfg string, w, h int) (*session.Session, func() *screen) {
	t.Helper()
	s, err := session.Start(session.Spec{
		ID:   "claudecontrol",
		Argv: []string{build(t), "-config", cfg},
		Dir:  ".",
		// The application records its panes on every layout change. Pointed at
		// a temporary directory so a test run never touches the state of the
		// person running it.
		Env:    []string{"XDG_STATE_HOME=" + t.TempDir()},
		Width:  w,
		Height: h,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, func() *screen {
		g := newScreen(w, h)
		s.Term.Draw(g, uv.Rect(0, 0, w, h))
		return g
	}
}

// waitForAnywhere polls until the text appears on any row. Panels are centred,
// so their position depends on the window size and on how many rows fit.
func waitForAnywhere(t *testing.T, snap func() *screen, want string) *screen {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		g := snap()
		for y := 0; y < g.h; y++ {
			if strings.Contains(g.row(y), want) {
				return g
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%q never appeared on screen", want)
	return nil
}

// columnOf returns the screen column where text starts on a row, or -1.
//
// Not the byte offset: the status bar carries multi-byte glyphs, so a byte
// index stopped matching a column the moment icons arrived. Widths come from
// the same measurement the renderer uses.
func columnOf(row, text string) int {
	i := strings.Index(row, text)
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(row[:i])
}

// anywhere reports whether the text is on screen right now.
func anywhere(g *screen, want string) bool {
	for y := 0; y < g.h; y++ {
		if strings.Contains(g.row(y), want) {
			return true
		}
	}
	return false
}

func waitForRow(t *testing.T, snap func() *screen, y int, want string) *screen {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var g *screen
	for time.Now().Before(deadline) {
		g = snap()
		if strings.Contains(g.row(y), want) {
			return g
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("row %d never contained %q; last value %q", y, want, g.row(y))
	return nil
}

// Two guests must appear side by side, separated by a divider, and stay there.
// Nothing writes after its first line, so this also pins down that a painted
// pane survives later frames.
func TestTwoPanesRenderSideBySideAndPersist(t *testing.T) {
	const W, H = 60, 12
	_, snap := run(t, "testdata/two-panes.yaml", W, H)

	g := waitForRow(t, snap, paneRow0, "RIGHTPANE")
	g = waitForRow(t, snap, paneRow0, "LEFTPANE")

	row := g.row(paneRow0)
	if !strings.HasPrefix(row, "LEFTPANE") {
		t.Errorf("row 0 = %q, want it to start with LEFTPANE", row)
	}
	left := strings.Index(row, "LEFTPANE")
	right := strings.Index(row, "RIGHTPANE")
	if left < 0 || right < 0 || left >= right {
		t.Errorf("row 0 = %q, want LEFTPANE to the left of RIGHTPANE", row)
	}

	// Neither guest writes again. A pane that is painted must stay painted.
	time.Sleep(500 * time.Millisecond)
	again := snap().row(paneRow0)
	if again != row {
		t.Errorf("row 0 changed with no guest output:\n before %q\n after  %q", row, again)
	}
}

// Focus decides where typing lands. Each guest echoes what it receives, so the
// pane a character appears in is the pane that had focus.
func TestFocusRoutesTypingToTheFocusedPane(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, "testdata/two-echo.yaml", W, H)

	waitForRow(t, snap, paneRow0, "L>")
	waitForRow(t, snap, paneRow0, "R>")

	// Focus starts on the first pane of the layout.
	s.SendText("A")
	time.Sleep(150 * time.Millisecond)
	waitForRow(t, snap, paneRow0, "L>A")

	// Alt+key reaches the terminal as ESC followed by the letter, and the
	// input decoder needs its escape timeout to elapse before it can tell that
	// pair from a bare Escape. Sending the next character too soon lets the
	// two runs of bytes coalesce, so the pause is part of the protocol, not
	// impatience.
	sendKey := func(keys string) {
		t.Helper()
		s.SendText(keys)
		time.Sleep(150 * time.Millisecond)
	}

	sendKey("\x1bl") // alt+l
	sendKey("B")
	waitForRow(t, snap, paneRow0, "R>B")

	sendKey("\x1bh") // alt+h
	sendKey("C")
	waitForRow(t, snap, paneRow0, "L>AC")
}

// click sends an SGR mouse press and release at a zero-based cell. SGR
// coordinates are one-based, hence the offsets.
//
// The two sequences differ only in their final byte, M for press and m for
// release. Emitted back to back they are decoded unreliably — the press is
// sometimes reported as a second release — so they are spaced out. No real
// mouse produces a press and a release in the same instant either.
func click(t *testing.T, s *session.Session, x, y int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1))
	time.Sleep(80 * time.Millisecond)
	s.SendText(fmt.Sprintf("\x1b[<0;%d;%dm", x+1, y+1))
	time.Sleep(150 * time.Millisecond)
}

// Clicking an unfocused pane must move focus there and swallow the click, so
// that changing panes can never trigger an action inside the pane you land on.
func TestClickFocusesAPaneWithoutReachingItsGuest(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, "testdata/two-echo.yaml", W, H)

	waitForRow(t, snap, paneRow0, "L>")
	waitForRow(t, snap, paneRow0, "R>")

	// Click well inside the right pane, which starts at column 30.
	click(t, s, 45, 5)
	s.SendText("B")
	time.Sleep(150 * time.Millisecond)
	waitForRow(t, snap, paneRow0, "R>B")

	// The guests echo everything they receive. Had the click been forwarded,
	// its escape sequence would have left a trace in the pane.
	row := snap().row(paneRow0)
	if strings.Contains(row, "[<0") || strings.Contains(row, "0;46;6") {
		t.Errorf("row 0 = %q, the click reached the guest", row)
	}

	// And back to the left pane.
	click(t, s, 5, 5)
	s.SendText("C")
	time.Sleep(150 * time.Millisecond)
	waitForRow(t, snap, paneRow0, "L>C")
}

// The status bar is the mouse-only path to everything the keyboard can do, so
// its labels are part of the contract.
func TestStatusBarOffersTheExpectedButtons(t *testing.T) {
	// Wide enough for every button. A narrower bar drops from the end on
	// purpose, which TestNarrowStatusBarDropsTheLeastImportantButtons covers.
	const W, H = 112, 14
	_, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")

	bar := waitForRow(t, snap, H-1, "quit").row(H - 1)
	for _, label := range []string{"new", "close", "list", "help", "set", "cmd", "zoom", "flip", "equal", "quit"} {
		if !strings.Contains(bar, label) {
			t.Errorf("status bar = %q, missing %q", bar, label)
		}
	}
	if !strings.Contains(bar, "2 panes") {
		t.Errorf("status bar = %q, want the pane count", bar)
	}
}

// Clicking help must open the panel, and the key that closes it must not reach
// the session underneath.
func TestHelpPanelOpensOnClickAndSwallowsTheKeyThatClosesIt(t *testing.T) {
	const W, H = 76, 20
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")
	waitForRow(t, snap, paneRow0, "R>")

	bar := waitForRow(t, snap, H-1, "help").row(H - 1)
	click(t, s, columnOf(bar, "help"), H-1)
	waitForAnywhere(t, snap, "SHORTCUTS")

	s.SendText("Z")
	time.Sleep(300 * time.Millisecond)
	if anywhere(snap(), "SHORTCUTS") {
		t.Fatal("the panel is still open")
	}
	if row := snap().row(paneRow0); strings.Contains(row, "L>Z") || strings.Contains(row, "R>Z") {
		t.Errorf("row 0 = %q, the dismissing key leaked into a session", row)
	}
}

// Quitting from the bar asks first: a stray click would otherwise end every
// session, and sessions do not survive the application.
func TestQuitButtonAsksBeforeEndingEverything(t *testing.T) {
	const W, H = 76, 20
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")

	bar := waitForRow(t, snap, H-1, "quit").row(H - 1)
	click(t, s, columnOf(bar, "quit"), H-1)
	waitForAnywhere(t, snap, "QUIT")

	// Anything but yes keeps the application alive.
	s.SendText("n")
	time.Sleep(300 * time.Millisecond)
	if st, _ := s.Status(); st == session.Exited {
		t.Fatal("answering no quit the application anyway")
	}
	waitForRow(t, snap, paneRow0, "L>")

	click(t, s, columnOf(bar, "quit"), H-1)
	waitForAnywhere(t, snap, "QUIT")
	s.SendText("y")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := s.Status(); st == session.Exited {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("answering yes did not quit")
}

// The panel must keep a margin on its right. Rows are drawn aligned on the
// longest key, so a row's width is that column plus its own text — measuring a
// row by its own key instead under-reports every short-key row and the text
// runs into the edge.
func TestHelpPanelKeepsItsRightMargin(t *testing.T) {
	const W, H = 78, 24
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")

	bar := waitForRow(t, snap, H-1, "help").row(H - 1)
	click(t, s, columnOf(bar, "help"), H-1)
	g := waitForAnywhere(t, snap, "SHORTCUTS")

	// The longest line in the panel decides its width.
	widest, wy := 0, -1
	for y := 0; y < g.h; y++ {
		if r := g.row(y); strings.Contains(r, "alt+") || strings.Contains(r, "click") {
			if w := ansi.StringWidth(r); w > widest {
				widest, wy = w, y
			}
		}
	}
	if wy < 0 {
		t.Fatal("no panel row found")
	}

	// Background-only cells are invisible in text, so the margin is read from
	// the styling: the panel must still be painted past its longest line.
	for _, dx := range []int{1, 2} {
		c := g.CellAt(widest+dx-1, wy)
		if c == nil || c.Style.Bg == nil {
			t.Fatalf("column %d of row %d is outside the panel; it ends flush with its text",
				widest+dx-1, wy)
		}
	}
}

// findRow returns the index of the first row containing text, or -1.
func findRow(g *screen, text string) int {
	for y := 0; y < g.h; y++ {
		if strings.Contains(g.row(y), text) {
			return y
		}
	}
	return -1
}

// Everything the keyboard can answer, the mouse must be able to answer too.
func TestPanelsAnswerToTheMouseAlone(t *testing.T) {
	const W, H = 78, 24
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")
	bar := waitForRow(t, snap, H-1, "quit").row(H - 1)

	// Help closes on its own button.
	click(t, s, columnOf(bar, "help"), H-1)
	g := waitForAnywhere(t, snap, "[ close ]")
	y := findRow(g, "[ close ]")
	click(t, s, columnOf(g.row(y), "[ close ]")+2, y)
	time.Sleep(300 * time.Millisecond)
	if anywhere(snap(), "SHORTCUTS") {
		t.Fatal("clicking close left the help panel open")
	}

	// Quit, answered with stay, keeps the application alive.
	click(t, s, columnOf(bar, "quit"), H-1)
	g = waitForAnywhere(t, snap, "[ stay ]")
	y = findRow(g, "[ stay ]")
	click(t, s, columnOf(g.row(y), "[ stay ]")+2, y)
	time.Sleep(400 * time.Millisecond)
	if st, _ := s.Status(); st == session.Exited {
		t.Fatal("clicking stay quit the application")
	}
	if anywhere(snap(), "[ stay ]") {
		t.Fatal("clicking stay left the confirmation open")
	}

	// Quit, answered with quit, ends it.
	click(t, s, columnOf(bar, "quit"), H-1)
	g = waitForAnywhere(t, snap, "[ quit ]")
	y = findRow(g, "[ quit ]")
	click(t, s, columnOf(g.row(y), "[ quit ]")+2, y)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := s.Status(); st == session.Exited {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("clicking quit did not end the application")
}

// A click anywhere else in the confirmation must be the safe answer.
func TestClickingOutsideTheQuitButtonsStays(t *testing.T) {
	const W, H = 78, 24
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")
	bar := waitForRow(t, snap, H-1, "quit").row(H - 1)

	click(t, s, columnOf(bar, "quit"), H-1)
	g := waitForAnywhere(t, snap, "QUIT")
	y := findRow(g, "QUIT")
	click(t, s, columnOf(g.row(y), "QUIT"), y)

	time.Sleep(400 * time.Millisecond)
	if st, _ := s.Status(); st == session.Exited {
		t.Fatal("a click away from the buttons quit the application")
	}
	if anywhere(snap(), "[ quit ]") {
		t.Error("the confirmation is still open")
	}
}

// A bar too narrow for every button drops from the end rather than letting
// labels overlap. Quit and the essentials survive; what goes is still on the
// keyboard and in the help panel.
func TestNarrowStatusBarDropsTheLeastImportantButtons(t *testing.T) {
	const W, H = 52, 14
	_, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")

	bar := waitForRow(t, snap, H-1, "quit").row(H - 1)
	for _, label := range []string{"new", "close", "quit"} {
		if !strings.Contains(bar, label) {
			t.Errorf("status bar = %q, dropped the essential %q", bar, label)
		}
	}
	if ansi.StringWidth(bar) > W {
		t.Errorf("status bar is %d columns wide on a %d column screen: %q",
			ansi.StringWidth(bar), W, bar)
	}
}

// The session list is the answer to "is Claude waiting for me anywhere?", so
// it must open, show what the pool holds, and close again without disturbing
// the panes behind it.
func TestSessionPanelOpensFromTheStatusBar(t *testing.T) {
	const W, H = 90, 24
	s, snap := run(t, "testdata/one-pane.yaml", W, H)
	waitForRow(t, snap, paneRow0, "P>")

	bar := waitForRow(t, snap, H-1, "list").row(H - 1)
	click(t, s, columnOf(bar, "list"), H-1)
	waitForAnywhere(t, snap, "SESSIONS")
	waitForAnywhere(t, snap, "enter attach")

	// The hosted command is in the pool, so the list must show it rather than
	// claiming there is nothing.
	if anywhere(snap(), "no sessions") {
		t.Error("the list is empty while a session is hosted")
	}

	s.SendText("\x1b") // escape
	time.Sleep(400 * time.Millisecond)
	if anywhere(snap(), "enter attach") {
		t.Fatal("escape left the session list open")
	}

	// The pane is still there, and still the pane.
	waitForRow(t, snap, paneRow0, "P>")
	s.SendText("A")
	waitForRow(t, snap, paneRow0, "P>A")
}

// The whole point of the hook bridge: a session that needs an answer says so,
// and the status bar shows it without anyone having to look at that pane.
//
// Claude Code is not involved. The hook command is our own binary, so the
// chain — socket, environment variable, payload, state mapping, status bar —
// can be exercised by invoking it exactly the way Claude Code would.
func TestAHookMarksASessionAsWaiting(t *testing.T) {
	const W, H = 90, 14
	runtimeDir := t.TempDir()
	bin := build(t)

	s, err := session.Start(session.Spec{
		ID:   "claudecontrol",
		Argv: []string{bin, "-config", "testdata/one-pane.yaml"},
		Dir:  ".",
		Env: []string{
			"XDG_STATE_HOME=" + t.TempDir(),
			"XDG_RUNTIME_DIR=" + runtimeDir,
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
	waitForRow(t, snap, H-1, "1 pane")

	socket := waitForSocket(t, runtimeDir)

	// A term pane names its session after the pane it lives in.
	cmd := exec.Command(bin, "--hook", "Notification")
	cmd.Env = append(os.Environ(), "CLAUDECONTROL_HOOK_SOCKET="+socket)
	cmd.Stdin = strings.NewReader(`{"session_id":"pane-1-1","cwd":"/tmp"}`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook: %v\n%s", err, out)
	}

	waitForRow(t, snap, H-1, "1 waiting")

	// And a Stop takes it back to quiet.
	cmd = exec.Command(bin, "--hook", "Stop")
	cmd.Env = append(os.Environ(), "CLAUDECONTROL_HOOK_SOCKET="+socket)
	cmd.Stdin = strings.NewReader(`{"session_id":"pane-1-1"}`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook: %v\n%s", err, out)
	}
	waitForRow(t, snap, H-1, "1 pane")
}

// waitForSocket waits for the application to open its hook socket.
func waitForSocket(t *testing.T, dir string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(dir, "claudecontrol-*.sock"))
		if len(matches) > 0 {
			return matches[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no hook socket appeared in %s", dir)
	return ""
}

// The hologram has to appear beside a live session without disturbing it. A
// braille glyph is the evidence; the session next to it must still take
// typing.
func TestHologramDrawsBesideALiveSession(t *testing.T) {
	const W, H = 90, 24
	s, snap := run(t, "testdata/hologram.yaml", W, H)
	waitForRow(t, snap, paneRow0, "P>")

	deadline := time.Now().Add(10 * time.Second)
	var found bool
	for time.Now().Before(deadline) && !found {
		g := snap()
		for y := 0; y < H-1 && !found; y++ {
			for _, r := range g.row(y) {
				if r >= 0x2801 && r <= 0x28FF {
					found = true
					break
				}
			}
		}
		if !found {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !found {
		t.Fatal("no braille glyph appeared; the hologram is not drawing")
	}

	// The session next to it is untouched.
	s.SendText("A")
	waitForRow(t, snap, paneRow0, "P>A")
}

// Opening the menu, moving a slider and saving is the whole feature. The
// comment in the file is the part that proves the write went through the
// syntax tree rather than re-encoding a struct.
func TestSettingsMenuAdjustsAndSaves(t *testing.T) {
	const W, H = 90, 24
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	body, err := os.ReadFile("testdata/settings.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, body, 0o600); err != nil {
		t.Fatal(err)
	}

	s, snap := run(t, cfg, W, H)

	bar := waitForRow(t, snap, H-1, "set").row(H - 1)
	click(t, s, columnOf(bar, "set"), H-1)
	waitForAnywhere(t, snap, "SETTINGS")
	waitForAnywhere(t, snap, "flow speed")

	// Move down to the speed slider and nudge it up.
	s.SendText("j")
	time.Sleep(150 * time.Millisecond)
	for i := 0; i < 5; i++ {
		s.SendText("l")
		time.Sleep(80 * time.Millisecond)
	}
	s.SendText("s")
	time.Sleep(600 * time.Millisecond)

	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if !strings.Contains(out, "# a hand-written note that must survive") {
		t.Errorf("the leading comment was lost:\n%s", out)
	}
	if strings.Contains(out, "speed: 0.18") {
		t.Errorf("the slider did not change anything:\n%s", out)
	}
}

// The control centre in one screen: a live session, service verdicts, and a
// task listed in a pane of its own.
func TestServicesAndTasksAppearBesideASession(t *testing.T) {
	const W, H = 100, 24
	_, snap := run(t, "testdata/control-centre.yaml", W, H)
	waitForRow(t, snap, paneRow0, "P>")

	// Both verdicts, so a failing check is visibly different from a passing one.
	waitForAnywhere(t, snap, "always-up")
	waitForAnywhere(t, snap, "always-down")
	waitForAnywhere(t, snap, "up")
	waitForAnywhere(t, snap, "down")

	// The task is listed before it is run.
	waitForAnywhere(t, snap, "greet")
}

// The palette has to reach an action by name, which is the point of having one.
func TestThePaletteRunsAnActionByName(t *testing.T) {
	const W, H = 100, 24
	s, snap := run(t, "testdata/control-centre.yaml", W, H)
	waitForRow(t, snap, paneRow0, "P>")
	waitForAnywhere(t, snap, "always-up")

	bar := waitForRow(t, snap, H-1, "cmd").row(H - 1)
	click(t, s, columnOf(bar, "cmd"), H-1)
	waitForAnywhere(t, snap, "›")

	// Type enough to single out zoom, then take it.
	for _, ch := range []string{"z", "o", "o", "m"} {
		s.SendText(ch)
		time.Sleep(80 * time.Millisecond)
	}
	waitForAnywhere(t, snap, "zoom / restore")
	s.SendText("\r")

	// Zoomed, the focused session fills the screen, so the services pane goes.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !anywhere(snap(), "always-up") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("choosing zoom from the palette did nothing")
}

// altClick sends an SGR click with the alt bit set. The button byte carries
// modifiers in its upper bits: 0b0000_1000 is alt.
func altClick(t *testing.T, s *session.Session, x, y int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<8;%d;%dM", x+1, y+1))
	time.Sleep(80 * time.Millisecond)
}

// drag sends a motion with a button held, which is what the terminal reports
// between a press and a release.
func drag(t *testing.T, s *session.Session, x, y int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<32;%d;%dM", x+1, y+1))
	time.Sleep(80 * time.Millisecond)
}

func release(t *testing.T, s *session.Session, x, y int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<0;%d;%dm", x+1, y+1))
	time.Sleep(300 * time.Millisecond)
}

// The gesture end to end. Typing afterwards is the only proof that the tree
// and the screen agree about where the pane went: a move that updated one and
// not the other would still look right until the first keystroke.
func TestAltDraggingAPaneMovesIt(t *testing.T) {
	const W, H = 80, 16
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")
	waitForRow(t, snap, paneRow0, "R>")

	// The left guest prompts at column 0, the right one past the divider.
	before := snap().row(paneRow0)
	leftAt := strings.Index(before, "L>")
	rightAt := strings.Index(before, "R>")
	if leftAt < 0 || rightAt < 0 || leftAt >= rightAt {
		t.Fatalf("row 0 = %q, want L> left of R>", before)
	}

	// Pick the left pane up and drop it on the right half of the right pane.
	altClick(t, s, 4, 5)
	drag(t, s, W-4, 5)
	waitForAnywhere(t, snap, "▸ right")
	release(t, s, W-4, 5)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		row := snap().row(paneRow0)
		l, r := strings.Index(row, "L>"), strings.Index(row, "R>")
		if l >= 0 && r >= 0 && r < l {
			// The panes swapped sides. Typing must reach the pane that was
			// dragged, which kept the focus.
			s.SendText("Z")
			waitForRow(t, snap, paneRow0, "L>Z")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("row 0 = %q; the pane did not move", snap().row(paneRow0))
}

// Escape puts it back down, and the layout is untouched.
func TestEscapeCancelsAPaneDrag(t *testing.T) {
	const W, H = 80, 16
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, paneRow0, "L>")
	before := snap().row(paneRow0)

	altClick(t, s, 4, 5)
	drag(t, s, W-4, 5)
	waitForAnywhere(t, snap, "▸ right")
	s.SendText("\x1b")
	time.Sleep(500 * time.Millisecond)

	if anywhere(snap(), "▸ right") {
		t.Fatal("the preview is still showing after escape")
	}
	if got := snap().row(paneRow0); got != before {
		t.Errorf("row 0 = %q after cancelling, want %q", got, before)
	}
}
