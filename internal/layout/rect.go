package layout

// Minimum usable size for a pane. A split that cannot honour it for every
// child is not laid out; the caller keeps the previous layout.
const (
	MinPaneW = 8
	MinPaneH = 3
)

// Rect is a screen rectangle in cells. X and Y are zero-based.
type Rect struct{ X, Y, W, H int }

// DividerRect is the one-cell strip between two siblings, plus enough context
// for a drag to know which ratios it adjusts.
type DividerRect struct {
	Rect   Rect
	Parent *Node
	Index  int // divider i sits between Children[i] and Children[i+1]
}

// Distribute splits total into len(ratios) parts proportional to ratios.
//
// It rounds cumulatively rather than per-part, which guarantees the parts sum
// to total exactly. Rounding each part independently leaves a stray cell at
// the edge whenever the division is not exact.
func Distribute(total int, ratios []int) []int {
	out := make([]int, len(ratios))
	if len(ratios) == 0 {
		return out
	}
	sum := 0
	for _, r := range ratios {
		sum += r
	}
	if sum <= 0 {
		return out
	}
	acc, prev := 0, 0
	for i, r := range ratios {
		acc += r
		cur := total * acc / sum
		out[i] = cur - prev
		prev = cur
	}
	return out
}

// Compute assigns a rectangle to every visible leaf. Leaves hidden inside a
// stack are absent from the result.
func Compute(root *Node, area Rect) map[PaneID]Rect {
	out := make(map[PaneID]Rect)
	place(root, area, out)
	return out
}

func place(n *Node, area Rect, out map[PaneID]Rect) {
	if n == nil || area.W <= 0 || area.H <= 0 {
		return
	}
	switch n.Kind {
	case KindLeaf:
		out[n.PaneID] = area
	case KindStack:
		if n.Active >= 0 && n.Active < len(n.Children) {
			place(n.Children[n.Active], area, out)
		}
	case KindSplit:
		for i, r := range splitAreas(n, area) {
			place(n.Children[i], r, out)
		}
	}
}

// splitAreas returns one rectangle per child, with a one-cell gap between
// consecutive children for the divider.
func splitAreas(n *Node, area Rect) []Rect {
	count := len(n.Children)
	if count == 0 {
		return nil
	}
	gaps := count - 1
	out := make([]Rect, count)
	if n.Orientation == Horizontal {
		parts := Distribute(area.W-gaps, n.Ratios)
		x := area.X
		for i, w := range parts {
			out[i] = Rect{X: x, Y: area.Y, W: w, H: area.H}
			x += w + 1
		}
		return out
	}
	parts := Distribute(area.H-gaps, n.Ratios)
	y := area.Y
	for i, h := range parts {
		out[i] = Rect{X: area.X, Y: y, W: area.W, H: h}
		y += h + 1
	}
	return out
}

// Dividers returns every draggable divider strip in the tree.
func Dividers(root *Node, area Rect) []DividerRect {
	var out []DividerRect
	collectDividers(root, area, &out)
	return out
}

func collectDividers(n *Node, area Rect, out *[]DividerRect) {
	if n == nil || area.W <= 0 || area.H <= 0 {
		return
	}
	switch n.Kind {
	case KindStack:
		if n.Active >= 0 && n.Active < len(n.Children) {
			collectDividers(n.Children[n.Active], area, out)
		}
	case KindSplit:
		areas := splitAreas(n, area)
		for i := 0; i < len(areas)-1; i++ {
			a := areas[i]
			var strip Rect
			if n.Orientation == Horizontal {
				strip = Rect{X: a.X + a.W, Y: a.Y, W: 1, H: a.H}
			} else {
				strip = Rect{X: a.X, Y: a.Y + a.H, W: a.W, H: 1}
			}
			*out = append(*out, DividerRect{Rect: strip, Parent: n, Index: i})
		}
		for i, r := range areas {
			collectDividers(n.Children[i], r, out)
		}
	}
}
