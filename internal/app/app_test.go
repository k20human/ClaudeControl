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

	g := waitForRow(t, snap, 0, "RIGHTPANE")
	g = waitForRow(t, snap, 0, "LEFTPANE")

	row := g.row(0)
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
	again := snap().row(0)
	if again != row {
		t.Errorf("row 0 changed with no guest output:\n before %q\n after  %q", row, again)
	}
}

// Focus decides where typing lands. Each guest echoes what it receives, so the
// pane a character appears in is the pane that had focus.
func TestFocusRoutesTypingToTheFocusedPane(t *testing.T) {
	const W, H = 60, 12
	s, snap := run(t, "testdata/two-echo.yaml", W, H)

	waitForRow(t, snap, 0, "L>")
	waitForRow(t, snap, 0, "R>")

	// Focus starts on the first pane of the layout.
	s.SendText("A")
	time.Sleep(150 * time.Millisecond)
	waitForRow(t, snap, 0, "L>A")

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
	waitForRow(t, snap, 0, "R>B")

	sendKey("\x1bh") // alt+h
	sendKey("C")
	waitForRow(t, snap, 0, "L>AC")
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

	waitForRow(t, snap, 0, "L>")
	waitForRow(t, snap, 0, "R>")

	// Click well inside the right pane, which starts at column 30.
	click(t, s, 45, 5)
	s.SendText("B")
	time.Sleep(150 * time.Millisecond)
	waitForRow(t, snap, 0, "R>B")

	// The guests echo everything they receive. Had the click been forwarded,
	// its escape sequence would have left a trace in the pane.
	row := snap().row(0)
	if strings.Contains(row, "[<0") || strings.Contains(row, "0;46;6") {
		t.Errorf("row 0 = %q, the click reached the guest", row)
	}

	// And back to the left pane.
	click(t, s, 5, 5)
	s.SendText("C")
	time.Sleep(150 * time.Millisecond)
	waitForRow(t, snap, 0, "L>C")
}

// The status bar is the mouse-only path to everything the keyboard can do, so
// its labels are part of the contract.
func TestStatusBarOffersTheExpectedButtons(t *testing.T) {
	const W, H = 100, 14
	_, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, 0, "L>")

	bar := waitForRow(t, snap, H-1, "quit").row(H - 1)
	for _, label := range []string{"new", "close", "list", "help", "zoom", "flip", "equal", "quit"} {
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
	waitForRow(t, snap, 0, "L>")
	waitForRow(t, snap, 0, "R>")

	bar := waitForRow(t, snap, H-1, "help").row(H - 1)
	click(t, s, columnOf(bar, "help"), H-1)
	waitForAnywhere(t, snap, "SHORTCUTS")

	s.SendText("Z")
	time.Sleep(300 * time.Millisecond)
	if anywhere(snap(), "SHORTCUTS") {
		t.Fatal("the panel is still open")
	}
	if row := snap().row(0); strings.Contains(row, "L>Z") || strings.Contains(row, "R>Z") {
		t.Errorf("row 0 = %q, the dismissing key leaked into a session", row)
	}
}

// Quitting from the bar asks first: a stray click would otherwise end every
// session, and sessions do not survive the application.
func TestQuitButtonAsksBeforeEndingEverything(t *testing.T) {
	const W, H = 76, 20
	s, snap := run(t, "testdata/two-echo.yaml", W, H)
	waitForRow(t, snap, 0, "L>")

	bar := waitForRow(t, snap, H-1, "quit").row(H - 1)
	click(t, s, columnOf(bar, "quit"), H-1)
	waitForAnywhere(t, snap, "QUIT")

	// Anything but yes keeps the application alive.
	s.SendText("n")
	time.Sleep(300 * time.Millisecond)
	if st, _ := s.Status(); st == session.Exited {
		t.Fatal("answering no quit the application anyway")
	}
	waitForRow(t, snap, 0, "L>")

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
	waitForRow(t, snap, 0, "L>")

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
	waitForRow(t, snap, 0, "L>")
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
	waitForRow(t, snap, 0, "L>")
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
	waitForRow(t, snap, 0, "L>")

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
