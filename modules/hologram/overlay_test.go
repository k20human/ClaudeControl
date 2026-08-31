package hologram

import (
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/module"
)

// drawn paints a hologram of a given readout mode and hands back the screen.
func drawn(t *testing.T, side string, w, h int) *grid {
	t.Helper()
	built, err := module.New("hologram", map[string]any{"style": "sphere", "readout": side})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m := built.(*Module)
	if err := m.Init(module.Context{PaneID: 1, Wake: func() {}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Resize(w, h); err != nil {
		t.Fatal(err)
	}
	m.readout.Note(time.Now(), "session started")
	for i := 0; i < 30; i++ {
		m.Step(1.0 / 30)
	}
	g := newGrid(w, h)
	m.Draw(g, uv.Rect(0, 0, w, h))
	return g
}

// countBraille is how much sphere is on screen.
func (g *grid) countBraille() int {
	n := 0
	for _, c := range g.cells {
		for _, r := range c.Content {
			if r >= 0x2801 && r <= 0x28FF {
				n++
			}
		}
	}
	return n
}

// A reserved column costs the sphere its width whether or not there is
// anything to put in it. Overlaid, the sphere has the whole pane and the text
// sits on top of it.
func TestOverlayGivesTheSphereTheWholePane(t *testing.T) {
	const w, h = 80, 30
	column := drawn(t, "right", w, h)
	over := drawn(t, "overlay", w, h)

	if over.countBraille() <= column.countBraille() {
		t.Errorf("overlaid draws %d braille cells against %d beside a column; "+
			"the sphere did not get the space back",
			over.countBraille(), column.countBraille())
	}
}

// And the text is still there to read.
func TestOverlayStillShowsWhatHappened(t *testing.T) {
	g := drawn(t, "overlay", 80, 30)
	if !strings.Contains(g.text(), "session started") {
		t.Errorf("the log is not on screen:\n%s", g.text())
	}
	if !strings.Contains(g.text(), "▸") {
		t.Errorf("the ambient line is not on screen:\n%s", g.text())
	}
}

// It takes only the rows it has lines for. A pane whose top-right corner is
// blanked to the bottom would be the reserved column with extra steps.
func TestOverlayTakesOnlyTheRowsItNeeds(t *testing.T) {
	const w, h = 80, 30
	g := drawn(t, "overlay", w, h)

	// Well below the two lines of text, the right-hand side belongs to the
	// sphere again.
	lower := 0
	for y := h / 2; y < h-1; y++ {
		for x := w / 2; x < w; x++ {
			for _, r := range g.cells[y*w+x].Content {
				if r >= 0x2801 && r <= 0x28FF {
					lower++
				}
			}
		}
	}
	if lower == 0 {
		t.Errorf("nothing is drawn below the text on the right:\n%s", g.text())
	}
}

// An unknown mode is still refused, so a typo is a message rather than a
// silently missing column.
func TestAnUnknownReadoutIsStillRefused(t *testing.T) {
	if _, err := module.New("hologram", map[string]any{"readout": "sideways"}); err == nil {
		t.Error("an unknown readout was accepted")
	}
}
