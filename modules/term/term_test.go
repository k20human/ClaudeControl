package term_test

import (
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	_ "claudecontrol/modules/term"
)

// buffer is a minimal uv.Screen so Draw can be tested without a terminal.
type buffer struct {
	w, h  int
	cells []uv.Cell
}

func newBuffer(w, h int) *buffer {
	b := &buffer{w: w, h: h, cells: make([]uv.Cell, w*h)}
	// A real screen starts out full of blanks. The zero Cell renders as an
	// empty string, not a space, so an unfilled buffer silently swallows the
	// columns a partial Draw never touches.
	for i := range b.cells {
		b.cells[i] = uv.EmptyCell
	}
	return b
}

func (b *buffer) Bounds() uv.Rectangle { return uv.Rect(0, 0, b.w, b.h) }

func (b *buffer) CellAt(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return nil
	}
	return &b.cells[y*b.w+x]
}

func (b *buffer) SetCell(x, y int, c *uv.Cell) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return
	}
	if c == nil {
		b.cells[y*b.w+x] = uv.EmptyCell
		return
	}
	b.cells[y*b.w+x] = *c
}

func (b *buffer) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }

func (b *buffer) row(y int) string {
	var sb strings.Builder
	for x := 0; x < b.w; x++ {
		sb.WriteString(b.cells[y*b.w+x].String())
	}
	return strings.TrimRight(sb.String(), " \x00")
}

func TestTermModuleDrawsGuestOutputAtTheGivenOffset(t *testing.T) {
	p := pool.New(bus.New())
	m, err := module.New("term", map[string]any{
		"cmd": []any{"printf", "hi"},
		"dir": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("module.New: %v", err)
	}
	if err := m.Init(module.Context{PaneID: 1, Pool: p, Wake: func() {}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer m.Close()

	if err := m.Resize(10, 3); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		buf := newBuffer(20, 6)
		m.Draw(buf, uv.Rect(5, 2, 10, 3))
		got = buf.row(2)
		if strings.Contains(got, "hi") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.HasPrefix(got, "     hi") {
		t.Fatalf("row 2 = %q, want the guest output starting at column 5", got)
	}
	if len(p.All()) != 1 {
		t.Fatalf("pool holds %d sessions, want 1", len(p.All()))
	}
}

func TestUnknownModuleNameIsAnError(t *testing.T) {
	if _, err := module.New("nope", nil); err == nil {
		t.Fatal("module.New(nope) = nil error, want an error")
	}
}

// A shifted character must reach the guest. The emulator's key encoder drops
// any key whose modifier is non-zero and does not match one of its hard-coded
// combinations, so routing printable text through it silently swallows every
// capital letter. This pins the workaround down.
func TestShiftedCharacterReachesTheGuest(t *testing.T) {
	p := pool.New(bus.New())
	m, err := module.New("term", map[string]any{
		"cmd": []any{"sh", "-c", "printf '>'; cat"},
		"dir": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("module.New: %v", err)
	}
	if err := m.Init(module.Context{PaneID: 1, Pool: p, Wake: func() {}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer m.Close()
	if err := m.Resize(20, 3); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	in, ok := m.(module.Inputter)
	if !ok {
		t.Fatal("the term module does not accept input")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		buf := newBuffer(20, 3)
		m.Draw(buf, uv.Rect(0, 0, 20, 3))
		if strings.HasPrefix(buf.row(0), ">") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Exactly what ultraviolet reports when the user presses shift+a.
	in.Key(uv.KeyPressEvent{Text: "A", Mod: uv.ModShift, Code: 'a', ShiftedCode: 'A'})

	var got string
	for time.Now().Before(deadline) {
		buf := newBuffer(20, 3)
		m.Draw(buf, uv.Rect(0, 0, 20, 3))
		got = buf.row(0)
		if strings.Contains(got, ">A") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("row 0 = %q, want the capital A to have reached the guest", got)
}

// A pane that hosts a session must say so. The list distinguishes a session on
// screen from one running in the background, and a module that adds to the
// pool without marking itself attached makes the list report the opposite of
// what is true.
func TestHostingAPaneMarksTheSessionAttached(t *testing.T) {
	p := pool.New(bus.New())
	m, err := module.New("term", map[string]any{
		"cmd": []any{"sleep", "30"},
		"dir": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("module.New: %v", err)
	}
	if err := m.Init(module.Context{PaneID: 7, Pool: p, Wake: func() {}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer m.Close()
	if err := m.Resize(20, 5); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	all := p.All()
	if len(all) != 1 {
		t.Fatalf("pool holds %d sessions, want 1", len(all))
	}
	if !all[0].Attached {
		t.Error("the session is in the pool but not marked attached")
	}
	// Two panes running the same command in the same directory must still be
	// tellable apart in the list.
	if all[0].Title != "sleep 7" {
		t.Errorf("title = %q, want it to carry the pane number", all[0].Title)
	}
}
