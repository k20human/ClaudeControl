package app_test

import (
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
		ID:     "claudecontrol",
		Argv:   []string{build(t), "-config", cfg},
		Dir:    ".",
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
	if !strings.Contains(row, "│") {
		t.Errorf("row 0 = %q, want a divider glyph", row)
	}
	left := strings.Index(row, "LEFTPANE")
	div := strings.Index(row, "│")
	right := strings.Index(row, "RIGHTPANE")
	if !(left < div && div < right) {
		t.Errorf("row 0 = %q, want LEFTPANE then divider then RIGHTPANE", row)
	}

	// Neither guest writes again. A pane that is painted must stay painted.
	time.Sleep(500 * time.Millisecond)
	again := snap().row(0)
	if again != row {
		t.Errorf("row 0 changed with no guest output:\n before %q\n after  %q", row, again)
	}
}
