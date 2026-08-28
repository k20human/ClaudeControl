package supervisor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/module"
	"claudecontrol/internal/relay"
)

// buildBinary builds the application, which is also the relay. A module made
// here is pointed at it: os.Executable in a test is the test binary, which
// knows nothing about --relay.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claudecontrol")
	cmd := exec.Command("go", "build", "-o", bin, "claudecontrol/cmd/claudecontrol")
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func keptModule(t *testing.T, bin, name, script, dir string) *Module {
	t.Helper()
	built, err := New(map[string]any{
		"keep_running": true,
		"services": []any{
			map[string]any{"name": name, "cmd": []any{"sh", "-c", script}, "dir": dir},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m := built.(*Module)
	m.binary = bin
	if err := m.Init(module.Context{Wake: func() {}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Resize(70, 12); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	return m
}

// pane is a screen an internal test can paint into. The external tests have
// one of their own; the two packages cannot share it.
type pane struct {
	w, h  int
	cells []uv.Cell
}

func newPane(w, h int) *pane {
	p := &pane{w: w, h: h, cells: make([]uv.Cell, w*h)}
	for i := range p.cells {
		p.cells[i] = uv.EmptyCell
	}
	return p
}

func (p *pane) Bounds() uv.Rectangle { return uv.Rect(0, 0, p.w, p.h) }

func (p *pane) CellAt(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= p.w || y >= p.h {
		return nil
	}
	return &p.cells[y*p.w+x]
}

func (p *pane) SetCell(x, y int, c *uv.Cell) {
	if x < 0 || y < 0 || x >= p.w || y >= p.h || c == nil {
		return
	}
	p.cells[y*p.w+x] = *c
}

func (p *pane) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }

func (p *pane) text() string {
	var b strings.Builder
	for y := 0; y < p.h; y++ {
		for x := 0; x < p.w; x++ {
			b.WriteString(p.cells[y*p.w+x].Content)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func screenOf(t *testing.T, m *Module) string {
	t.Helper()
	g := newPane(70, 12)
	m.Draw(g, uv.Rect(0, 0, 70, 12))
	return g.text()
}

func running(m *Module, name string) Snapshot {
	for _, s := range m.Services() {
		if s.Name == name {
			return s
		}
	}
	return Snapshot{Name: "<missing>"}
}

// The whole point: closing the application leaves the service running, and
// opening it again finds it — with its output, which is what a service adopted
// from a bare process cannot offer.
func TestAKeptServiceOutlivesTheApplicationAndKeepsItsOutput(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	bin := buildBinary(t)
	work := t.TempDir()

	first := keptModule(t, bin, "kept",
		"i=0; while true; do i=$((i+1)); echo \"tick $i\"; sleep 0.1; done", work)
	first.StartPicked()
	until(t, "the service", func() bool { return running(first, "kept").State == Running })
	// The pane shows the list until a service is opened; the output is what
	// this is about.
	first.show(0)
	until(t, "its output", func() bool { return strings.Contains(screenOf(t, first), "tick 1") })

	pid := running(first, "kept").Pid
	if pid <= 0 {
		t.Fatal("no pid for a running service")
	}

	// The application closes. Nothing is signalled.
	_ = first.Close()
	time.Sleep(600 * time.Millisecond)
	if !aliveNow(pid) {
		t.Fatal("closing the application ended the service")
	}

	// And it is still printing, which is what proves nothing is blocked on a
	// terminal nobody is reading.
	logPath := filepath.Join(first.relayDir("kept"), relay.LogFile)
	was := sizeOf(logPath)
	time.Sleep(700 * time.Millisecond)
	if now := sizeOf(logPath); now <= was {
		t.Fatalf("the service stopped writing (%d then %d)", was, now)
	}

	// A new run finds it.
	second := keptModule(t, bin, "kept",
		"i=0; while true; do i=$((i+1)); echo \"tick $i\"; sleep 0.1; done", work)
	t.Cleanup(func() { second.stopAll() })
	got := running(second, "kept")
	if got.State != Running {
		t.Fatalf("the second run says %v, want running", got.State)
	}
	if got.Pid != pid {
		t.Errorf("Pid = %d, want the service that was already there (%d)", got.Pid, pid)
	}
	second.show(0)
	until(t, "the output to come back", func() bool {
		return strings.Contains(screenOf(t, second), "tick ")
	})
}

// Colours survive the round trip: the log holds the bytes the terminal
// carried, and replaying them through an emulator puts them back.
func TestAKeptServiceKeepsItsColours(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	bin := buildBinary(t)

	m := keptModule(t, bin, "painted",
		"printf '\\033[31mRED-HERE\\033[0m\\n'; sleep 30", t.TempDir())
	t.Cleanup(func() { m.stopAll() })
	m.StartPicked()
	until(t, "the service", func() bool { return running(m, "painted").State == Running })
	m.show(0)

	g := newPane(70, 12)
	until(t, "the output", func() bool {
		g = newPane(70, 12)
		m.Draw(g, uv.Rect(0, 0, 70, 12))
		return strings.Contains(g.text(), "RED-HERE")
	})

	// Red, not the colour everything else is drawn in.
	found := false
	for y := 0; y < 12; y++ {
		for x := 0; x < 70; x++ {
			c := g.CellAt(x, y)
			if c == nil || c.Content != "R" {
				continue
			}
			r, gg, b, _ := c.Style.Fg.RGBA()
			if r > gg && r > b {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("the output came back without its colour:\n%s", g.text())
	}
}

// Stopping one means stopping it, relay or no relay.
func TestAKeptServiceStopsWhenAsked(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	bin := buildBinary(t)

	m := keptModule(t, bin, "stoppable", "sleep 300", t.TempDir())
	m.StartPicked()
	until(t, "the service", func() bool { return running(m, "stoppable").State == Running })
	pid := running(m, "stoppable").Pid

	m.StopPicked()
	until(t, "it to go", func() bool { return !aliveNow(pid) })
	if got := running(m, "stoppable").State; got != Stopped {
		t.Errorf("state = %v, want stopped", got)
	}
}

func sizeOf(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// stopAll ends every service a test started, relay included.
func (m *Module) stopAll() {
	m.mu.Lock()
	for _, s := range m.svcs {
		s.picked = true
	}
	m.mu.Unlock()
	m.StopPicked()
	time.Sleep(300 * time.Millisecond)
	_ = m.Close()
}

// A log is text you read, and text you read is text you copy.
func TestTheLogCanBeSelectedAndCopied(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	bin := buildBinary(t)

	m := keptModule(t, bin, "wordy", "echo COPY-THIS-LINE; sleep 30", t.TempDir())
	t.Cleanup(func() { m.stopAll() })
	m.StartPicked()
	until(t, "the service", func() bool { return running(m, "wordy").State == Running })
	m.show(0)

	g := newPane(70, 12)
	until(t, "the output", func() bool {
		g = newPane(70, 12)
		m.Draw(g, uv.Rect(0, 0, 70, 12))
		return strings.Contains(g.text(), "COPY-THIS-LINE")
	})

	// Where the words are, in the pane's own coordinates.
	row, col := -1, -1
	for y := 0; y < 12 && row < 0; y++ {
		line := ""
		for x := 0; x < 70; x++ {
			line += g.cells[y*70+x].Content
		}
		if c := strings.Index(line, "COPY-THIS-LINE"); c >= 0 {
			row, col = y, c
		}
	}
	if row < 0 {
		t.Fatalf("the line is not on screen:\n%s", g.text())
	}

	m.Mouse(uv.MouseClickEvent{X: col, Y: row, Button: uv.MouseLeft})
	m.Mouse(uv.MouseMotionEvent{X: col + len("COPY-THIS-LINE") - 1, Y: row, Button: uv.MouseLeft})
	m.Mouse(uv.MouseReleaseEvent{X: col + len("COPY-THIS-LINE") - 1, Y: row, Button: uv.MouseLeft})

	if got := m.SelectedText(); !strings.Contains(got, "COPY-THIS-LINE") {
		t.Errorf("SelectedText = %q, want the line that was dragged across", got)
	}
}
