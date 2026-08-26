package hologram_test

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/module"
	"claudecontrol/modules/hologram"
)

type buffer struct {
	w, h  int
	cells []uv.Cell
}

func newBuffer(w, h int) *buffer {
	b := &buffer{w: w, h: h, cells: make([]uv.Cell, w*h)}
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
	return sb.String()
}

// countBraille reports how many cells hold a braille glyph.
func (b *buffer) countBraille() int {
	n := 0
	for _, c := range b.cells {
		for _, r := range c.Content {
			if r >= 0x2801 && r <= 0x28FF {
				n++
			}
		}
	}
	return n
}

func draw(t *testing.T, cfg map[string]any, w, h, frames int) *buffer {
	t.Helper()
	m, err := module.New("hologram", cfg)
	if err != nil {
		t.Fatalf("module.New: %v", err)
	}
	if err := m.Init(module.Context{PaneID: 1, Wake: func() {}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Resize(w, h); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	stepper, ok := m.(interface{ Step(float64) })
	if !ok {
		t.Fatal("the hologram module cannot be stepped")
	}
	for i := 0; i < frames; i++ {
		stepper.Step(1.0 / 30)
	}
	b := newBuffer(w+10, h+4)
	m.Draw(b, uv.Rect(3, 2, w, h))
	return b
}

// The sphere must actually appear, and appear only inside the area it was
// given: a module that paints outside its rectangle paints over a live
// session.
func TestSphereDrawsInsideItsAreaOnly(t *testing.T) {
	const w, h = 30, 15
	b := draw(t, map[string]any{"style": "sphere"}, w, h, 200)

	if n := b.countBraille(); n < 20 {
		t.Fatalf("only %d braille cells drawn; the sphere is not appearing", n)
	}
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			inside := x >= 3 && x < 3+w && y >= 2 && y < 2+h
			if inside {
				continue
			}
			if c := b.CellAt(x, y); c != nil && c.Content != " " && c.Content != "" {
				t.Fatalf("cell (%d,%d) was painted outside the area: %q", x, y, c.Content)
			}
		}
	}
}

func TestEveryStyleDrawsSomething(t *testing.T) {
	for _, style := range []string{"sphere", "ring", "avatar"} {
		b := draw(t, map[string]any{"style": style}, 30, 15, 120)
		painted := 0
		for _, c := range b.cells {
			if c.Content != " " && c.Content != "" {
				painted++
			}
		}
		if painted == 0 {
			t.Errorf("style %q painted nothing", style)
		}
	}
}

func TestUnknownStyleIsAnError(t *testing.T) {
	if _, err := module.New("hologram", map[string]any{"style": "spiral"}); err == nil {
		t.Fatal("an unknown style was accepted")
	}
}

// The colour ramp is what carries depth. It must run dark to light without
// going backwards, or the far side of the sphere would read as the near one.
func TestRampBrightensMonotonically(t *testing.T) {
	last := -1
	for i := 0; i <= 20; i++ {
		r, g, bl := hologram.Ramp(float32(i) / 20)
		sum := r + g + bl
		if sum < last {
			t.Fatalf("ramp dimmed at %g: %d after %d", float32(i)/20, sum, last)
		}
		last = sum
	}
}
