package session_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/session"
)

// waitFor polls cond until it holds or the deadline passes. The PTY pump is
// asynchronous, so every assertion about emulator content needs this.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// gridBuffer is a minimal uv.Screen used to snapshot the emulator.
//
// The grid is never read through CellAt: that method returns a pointer into
// the live buffer and releases its lock on return, so any use of the result
// races with the PTY pump. Draw copies cells into a destination screen while
// holding the lock, which is the only safe way to read the grid from another
// goroutine — and it is exactly what the application does when rendering.
type gridBuffer struct {
	w, h  int
	cells []uv.Cell
}

func newGrid(w, h int) *gridBuffer {
	b := &gridBuffer{w: w, h: h, cells: make([]uv.Cell, w*h)}
	// A real screen starts out full of blanks. The zero Cell renders as an
	// empty string, not a space, so an unfilled buffer silently swallows the
	// columns a partial Draw never touches.
	for i := range b.cells {
		b.cells[i] = uv.EmptyCell
	}
	return b
}

func (b *gridBuffer) Bounds() uv.Rectangle { return uv.Rect(0, 0, b.w, b.h) }

func (b *gridBuffer) CellAt(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return nil
	}
	return &b.cells[y*b.w+x]
}

func (b *gridBuffer) SetCell(x, y int, c *uv.Cell) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return
	}
	if c == nil {
		b.cells[y*b.w+x] = uv.EmptyCell
		return
	}
	b.cells[y*b.w+x] = *c
}

func (b *gridBuffer) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }

func (b *gridBuffer) row(y int) string {
	var sb strings.Builder
	for x := 0; x < b.w; x++ {
		sb.WriteString(b.cells[y*b.w+x].String())
	}
	return strings.TrimRight(sb.String(), " \x00")
}

// snapshot copies the emulator grid into a private buffer.
func snapshot(s *session.Session, w, h int) *gridBuffer {
	g := newGrid(w, h)
	s.Term.Draw(g, uv.Rect(0, 0, w, h))
	return g
}

// anyRowContains reports whether text appears anywhere on the grid.
func anyRowContains(s *session.Session, w, h int, text string) bool {
	g := snapshot(s, w, h)
	for y := 0; y < h; y++ {
		if strings.Contains(g.row(y), text) {
			return true
		}
	}
	return false
}

func TestSessionRendersProcessOutput(t *testing.T) {
	s, err := session.Start(session.Spec{
		ID:     "render",
		Argv:   []string{"printf", "hello"},
		Dir:    t.TempDir(),
		Width:  20,
		Height: 5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	waitFor(t, `"hello" on row 0`, func() bool { return snapshot(s, 20, 5).row(0) == "hello" })
}

func TestSessionReportsExitCode(t *testing.T) {
	s, err := session.Start(session.Spec{
		ID:     "exit",
		Argv:   []string{"sh", "-c", "exit 3"},
		Dir:    t.TempDir(),
		Width:  20,
		Height: 5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	waitFor(t, "the process to exit", func() bool {
		st, _ := s.Status()
		return st == session.Exited
	})
	if _, code := s.Status(); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
}

// The guest must learn its new size. Polling stty avoids depending on how a
// given shell handles a signal that arrives during a builtin.
func TestSessionResizePropagatesToTheProcess(t *testing.T) {
	s, err := session.Start(session.Spec{
		ID:     "resize",
		Argv:   []string{"sh", "-c", "while :; do stty size; sleep 0.2; done"},
		Dir:    t.TempDir(),
		Width:  20,
		Height: 5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	waitFor(t, `the initial "5 20"`, func() bool { return anyRowContains(s, 20, 5, "5 20") })

	if err := s.Resize(40, 12); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	waitFor(t, `"12 40" after the resize`, func() bool { return anyRowContains(s, 40, 12, "12 40") })
}

func TestSessionSendTextReachesTheProcess(t *testing.T) {
	s, err := session.Start(session.Spec{
		ID:     "input",
		Argv:   []string{"sh", "-c", "read line; printf 'got:%s' \"$line\""},
		Dir:    t.TempDir(),
		Width:  30,
		Height: 5,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	time.Sleep(100 * time.Millisecond)
	s.SendText("ping\r")
	waitFor(t, `"got:ping"`, func() bool { return anyRowContains(s, 30, 5, "got:ping") })
}

// A resize drops every damage mark in the emulator, and Draw only copies
// lines that are marked. The guest here writes once and then does nothing, so
// nothing would bring the text back: the pane would go blank the moment it
// changed size. The session repaints the screen from its own snapshot to stop
// that happening.
func TestResizeKeepsWhatTheGuestAlreadyDrew(t *testing.T) {
	s, err := session.Start(session.Spec{
		ID:     "keep",
		Argv:   []string{"sh", "-c", "printf '\\033[1;32mgreen marker\\033[m'; sleep 30"},
		Width:  40,
		Height: 6,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	waitFor(t, "the marker", func() bool { return anyRowContains(s, 40, 6, "green marker") })

	for _, size := range [][2]int{{50, 6}, {50, 6}, {24, 6}} {
		if err := s.Resize(size[0], size[1]); err != nil {
			t.Fatalf("Resize(%d,%d): %v", size[0], size[1], err)
		}
		// Drawn into a fresh grid, so only what the emulator reports as
		// damaged can appear.
		if !anyRowContains(s, size[0], size[1], "green marker") {
			t.Fatalf("the marker vanished after resizing to %dx%d", size[0], size[1])
		}
	}
}

// The repaint paints whole lines, and a line that reaches the last column arms
// the terminal's pending-wrap flag. x/vt's DECRC restores the saved cursor
// position without clearing that flag, so a repaint that left it armed would
// push the guest's next character onto the following row — and every one after
// it, for the rest of the session. Automatic wrapping is switched off for the
// repaint to keep the flag from ever being armed.
func TestResizeLeavesTheCursorWhereTheGuestLeftIt(t *testing.T) {
	s, err := session.Start(session.Spec{
		ID:     "cursor",
		Argv:   []string{"sh", "-c", "printf 'L>'; cat"},
		Width:  39,
		Height: 4,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	waitFor(t, "the prompt", func() bool { return anyRowContains(s, 39, 4, "L>") })
	if err := s.Resize(40, 4); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	s.SendText("Z")
	waitFor(t, "the echo next to the prompt", func() bool {
		return snapshot(s, 40, 4).row(0) == "L>Z"
	})
}

// A development server is usually a launcher: `npm run dev` starts the real
// server as a child. Signalling only the child leaves that server holding its
// port, which is the failure that makes a supervisor useless — so the whole
// process group has to go.
func TestTerminateTakesTheWholeProcessGroup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child.pid")
	// The shell writes its child's pid and then waits, exactly as a launcher
	// would.
	s, err := session.Start(session.Spec{
		ID:     "group",
		Argv:   []string{"sh", "-c", "sleep 300 & echo $! > " + marker + "; wait"},
		Width:  40,
		Height: 6,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	var child int
	waitFor(t, "the child's pid", func() bool {
		raw, err := os.ReadFile(marker)
		if err != nil {
			return false
		}
		child, err = strconv.Atoi(strings.TrimSpace(string(raw)))
		return err == nil && child > 0
	})
	if !alive(child) {
		t.Fatalf("the child %d was never running", child)
	}

	if err := s.Terminate(2 * time.Second); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	waitFor(t, "the child to go", func() bool { return !alive(child) })
}

// alive reports whether a pid still exists. Signal zero performs no signal but
// still checks that the process is there.
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// A guest on the normal screen pushes its past into the scrollback, and that
// past is what the wheel should reach. Scrolling up has to show it, in order,
// above what is still on screen.
func TestDrawScrolledShowsWhatScrolledOff(t *testing.T) {
	const w, h = 20, 5
	s, err := session.Start(session.Spec{
		ID: "history", Width: w, Height: h,
		// Twelve numbered lines through a five-row screen: seven have gone.
		Argv: []string{"sh", "-c", "for i in 1 2 3 4 5 6 7 8 9 10 11 12; do echo line-$i; done; sleep 30"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	waitFor(t, "the last line", func() bool { return anyRowContains(s, w, h, "line-12") })
	waitFor(t, "the history", func() bool { return s.History() >= 7 })

	if s.AltScreen() {
		t.Fatal("a plain shell is reported as being on the alternate screen")
	}

	// At the bottom, only the tail is visible.
	live := scrolled(s, w, h, 0)
	if strings.Contains(live, "line-3") {
		t.Errorf("an old line is on the live screen:\n%s", live)
	}

	// Scrolled up by four, the four lines above the screen appear at the top
	// and the screen's own first rows follow.
	back := scrolled(s, w, h, 4)
	for _, want := range []string{"line-5", "line-8"} {
		if !strings.Contains(back, want) {
			t.Errorf("scrolled back four lines, %q is missing:\n%s", want, back)
		}
	}
	// And the bottom of the screen has been pushed out of view.
	if strings.Contains(back, "line-12") {
		t.Errorf("scrolling back kept the newest line:\n%s", back)
	}

	// Asking for more history than there is shows all of it rather than
	// failing or drawing blank rows.
	deep := scrolled(s, w, h, 9999)
	if !strings.Contains(deep, "line-1") {
		t.Errorf("the oldest line is unreachable:\n%s", deep)
	}
}

// scrolled renders a session at an offset and returns it as text.
func scrolled(s *session.Session, w, h, offset int) string {
	g := newGrid(w, h)
	s.DrawScrolled(g, uv.Rect(0, 0, w, h), offset)
	var b strings.Builder
	for y := 0; y < h; y++ {
		b.WriteString(g.row(y))
		b.WriteByte('\n')
	}
	return b.String()
}
