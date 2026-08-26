package holo_test

import (
	"testing"

	"claudecontrol/internal/holo"
)

// Braille packs two dots across and four down into one cell; half-blocks pack
// one across and two down. The dot grid has to match the mode, or the sphere
// comes out squashed.
func TestDotsForMatchesTheMode(t *testing.T) {
	if w, h := holo.DotsFor(holo.ModeBraille, 40, 20); w != 80 || h != 80 {
		t.Errorf("braille grid = %dx%d, want 80x80", w, h)
	}
	if w, h := holo.DotsFor(holo.ModeHalfBlock, 40, 20); w != 40 || h != 40 {
		t.Errorf("half-block grid = %dx%d, want 40x40", w, h)
	}
}

// The braille bit layout is historical, not row-major: the fourth row uses the
// two high bits. Getting it wrong shifts a quarter of every glyph.
func TestBrailleBitLayout(t *testing.T) {
	const w, h = 2, 4
	cases := []struct {
		x, y int
		want rune
	}{
		{0, 0, 0x2801}, {0, 1, 0x2802}, {0, 2, 0x2804}, {0, 3, 0x2840},
		{1, 0, 0x2808}, {1, 1, 0x2810}, {1, 2, 0x2820}, {1, 3, 0x2880},
	}
	for _, c := range cases {
		dots := make([]float32, w*h)
		dots[c.y*w+c.x] = 1
		got, _ := holo.BrailleCell(dots, w, h, 0, 0, holo.Threshold)
		if got != c.want {
			t.Errorf("dot (%d,%d) gave %U, want %U", c.x, c.y, got, c.want)
		}
	}
}

// A cell with nothing above the threshold is blank, so faint noise does not
// fill the pane with punctuation.
func TestBrailleCellIsBlankBelowTheThreshold(t *testing.T) {
	dots := make([]float32, 8)
	for i := range dots {
		dots[i] = holo.Threshold / 2
	}
	got, peak := holo.BrailleCell(dots, 2, 4, 0, 0, holo.Threshold)
	if got != 0x2800 {
		t.Errorf("cell = %U, want an empty braille cell", got)
	}
	if peak != 0 {
		t.Errorf("peak = %g, want 0 for a blank cell", peak)
	}
}

// The peak drives the colour, so it must be the brightest lit dot rather than
// an average that washes a vivid filament out.
func TestBrailleCellReportsItsBrightestDot(t *testing.T) {
	dots := []float32{0.3, 0.9, 0.25, 0, 0, 0, 0, 0}
	_, peak := holo.BrailleCell(dots, 2, 4, 0, 0, holo.Threshold)
	if peak != 0.9 {
		t.Fatalf("peak = %g, want 0.9", peak)
	}
}

// Cells at the edge must not read outside the grid.
func TestCellsClipAtTheEdges(t *testing.T) {
	dots := make([]float32, 6) // 2 x 3, one row short of a braille cell
	if r, _ := holo.BrailleCell(dots, 2, 3, 0, 0, holo.Threshold); r != 0x2800 {
		t.Errorf("cell = %U, want an empty cell rather than a panic", r)
	}
	if u, l := holo.HalfBlockCell(dots, 2, 3, 0, 1); u != 0 || l != 0 {
		t.Errorf("half-block past the edge = %g/%g, want zeroes", u, l)
	}
}
