package session_test

import (
	"strings"
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
	return &gridBuffer{w: w, h: h, cells: make([]uv.Cell, w*h)}
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

func TestRegistryTracksSessions(t *testing.T) {
	r := session.NewRegistry()
	s, err := session.Start(session.Spec{
		ID: "reg", Argv: []string{"sleep", "5"}, Dir: t.TempDir(), Width: 10, Height: 3,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.Add(s)
	if got, ok := r.Get("reg"); !ok || got != s {
		t.Fatal("Get(reg) did not return the session")
	}
	if len(r.All()) != 1 {
		t.Fatalf("All() = %d sessions, want 1", len(r.All()))
	}
	if err := r.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	if len(r.All()) != 0 {
		t.Fatalf("All() after CloseAll = %d, want 0", len(r.All()))
	}
}
