package app

import "claudecontrol/internal/layout"

// Direction is a focus movement axis.
type Direction int

const (
	// Left moves focus towards smaller X.
	Left Direction = iota
	// Right moves focus towards larger X.
	Right
	// Up moves focus towards smaller Y.
	Up
	// Down moves focus towards larger Y.
	Down
)

// nearest returns the pane to move to, or from when there is none.
//
// Candidates must lie strictly on the requested side and overlap the source on
// the other axis. Among those, the closest edge wins, then the closest centre,
// then position and finally identity. The last two look pedantic but they are
// what makes the result deterministic: a pane spanning two others is exactly
// equidistant from both, and without a final tie-break the same keystroke
// would land somewhere different each time.
func nearest(rects map[layout.PaneID]layout.Rect, from layout.PaneID, d Direction) layout.PaneID {
	src, ok := rects[from]
	if !ok {
		return from
	}
	best := from
	bestGap, bestOff := 1<<30, 1<<30
	bestRect := layout.Rect{X: 1 << 30, Y: 1 << 30}

	for id, r := range rects {
		if id == from {
			continue
		}
		var gap, off int
		switch d {
		case Left:
			if r.X+r.W > src.X || !overlaps(r.Y, r.H, src.Y, src.H) {
				continue
			}
			gap = src.X - (r.X + r.W)
			off = abs(centre(r.Y, r.H) - centre(src.Y, src.H))
		case Right:
			if r.X < src.X+src.W || !overlaps(r.Y, r.H, src.Y, src.H) {
				continue
			}
			gap = r.X - (src.X + src.W)
			off = abs(centre(r.Y, r.H) - centre(src.Y, src.H))
		case Up:
			if r.Y+r.H > src.Y || !overlaps(r.X, r.W, src.X, src.W) {
				continue
			}
			gap = src.Y - (r.Y + r.H)
			off = abs(centre(r.X, r.W) - centre(src.X, src.W))
		case Down:
			if r.Y < src.Y+src.H || !overlaps(r.X, r.W, src.X, src.W) {
				continue
			}
			gap = r.Y - (src.Y + src.H)
			off = abs(centre(r.X, r.W) - centre(src.X, src.W))
		}
		if better(gap, off, r, id, bestGap, bestOff, bestRect, best) {
			best, bestGap, bestOff, bestRect = id, gap, off, r
		}
	}
	return best
}

// better reports whether a candidate beats the incumbent, comparing in order:
// edge distance, centre offset, position, then pane id.
func better(gap, off int, r layout.Rect, id layout.PaneID,
	bestGap, bestOff int, bestRect layout.Rect, best layout.PaneID) bool {
	switch {
	case gap != bestGap:
		return gap < bestGap
	case off != bestOff:
		return off < bestOff
	case r.X != bestRect.X:
		return r.X < bestRect.X
	case r.Y != bestRect.Y:
		return r.Y < bestRect.Y
	default:
		return id < best
	}
}

func overlaps(a, alen, b, blen int) bool { return a < b+blen && b < a+alen }

func centre(pos, length int) int { return pos*2 + length }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
