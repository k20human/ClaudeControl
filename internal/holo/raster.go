package holo

// Mode is how a dot grid is packed into terminal cells.
type Mode int

const (
	// ModeBraille packs 2x4 dots per cell. Highest resolution, one colour per
	// cell — chosen because the sphere lives in brightness gradients that a
	// per-cell colour still conveys.
	ModeBraille Mode = iota
	// ModeHalfBlock packs 1x2 pixels per cell, each with its own colour.
	ModeHalfBlock
)

// Threshold is how bright a dot must be to be drawn at all.
const Threshold = 0.20

// brailleBit maps a dot within a cell to its bit. The layout is historical
// rather than row-major: the fourth row uses the two high bits, which is why
// this table exists instead of a shift.
var brailleBit = [4][2]byte{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// DotsFor returns the dot grid a pane of this many cells needs.
//
// Braille dots come out very nearly square: a cell is about twice as tall as
// it is wide, and braille divides it two across by four down. No aspect
// correction is needed anywhere else because of that.
func DotsFor(mode Mode, cols, rows int) (w, h int) {
	if mode == ModeHalfBlock {
		return cols, rows * 2
	}
	return cols * 2, rows * 4
}

// BrailleCell returns the glyph for one cell and the brightest dot in it.
func BrailleCell(dots []float32, w, h, cx, cy int, threshold float32) (rune, float32) {
	var mask byte
	var peak float32
	for dy := 0; dy < 4; dy++ {
		y := cy*4 + dy
		if y >= h {
			break
		}
		for dx := 0; dx < 2; dx++ {
			x := cx*2 + dx
			if x >= w {
				continue
			}
			v := dots[y*w+x]
			if v > threshold {
				mask |= brailleBit[dy][dx]
				if v > peak {
					peak = v
				}
			}
		}
	}
	return rune(0x2800 + int(mask)), peak
}

// HalfBlockCell returns the two pixel values a half-block cell covers.
func HalfBlockCell(dots []float32, w, h, cx, cy int) (upper, lower float32) {
	at := func(py int) float32 {
		if py >= h || cx >= w {
			return 0
		}
		return dots[py*w+cx]
	}
	return at(cy * 2), at(cy*2 + 1)
}
