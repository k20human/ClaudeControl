package app

import (
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/render"
)

var (
	dropFill   = color.RGBA{R: 0x1e, G: 0x3a, B: 0x4a, A: 0xff}
	dropEdge   = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	dropSwapBg = color.RGBA{R: 0x3a, G: 0x2e, B: 0x1e, A: 0xff}
	dropSwapFg = color.RGBA{R: 0xe5, G: 0x93, B: 0x3a, A: 0xff}
)

// centreHalfSpan is how far out from the middle still counts as the centre,
// as a fraction of the half-width. A pane is a rectangle but the zones are
// judged in normalised coordinates, so this is a diamond rather than a box —
// which is what makes the four edges meet cleanly at the corners.
const centreHalfSpan = 0.5

// paneDragState is a pane in flight.
type paneDragState struct {
	src    layout.PaneID
	target layout.PaneID
	side   layout.Side
}

// dropZone says where a pointer inside a pane would put a dropped pane.
//
// The position is normalised to [-1,1] on each axis and the axis you are
// proportionally further along wins. Judging in cells instead would make the
// zones of a wide pane behave differently from those of a tall one.
func dropZone(r layout.Rect, x, y int) layout.Side {
	if r.W <= 0 || r.H <= 0 {
		return layout.SideSwap
	}
	nx := (float64(x-r.X)+0.5)/float64(r.W)*2 - 1
	ny := (float64(y-r.Y)+0.5)/float64(r.H)*2 - 1

	ax, ay := math.Abs(nx), math.Abs(ny)
	if ax < centreHalfSpan && ay < centreHalfSpan {
		return layout.SideSwap
	}
	if ax >= ay {
		if nx < 0 {
			return layout.SideLeft
		}
		return layout.SideRight
	}
	if ny < 0 {
		return layout.SideTop
	}
	return layout.SideBottom
}

// previewRect is the region a dropped pane would occupy.
func previewRect(r layout.Rect, side layout.Side) layout.Rect {
	switch side {
	case layout.SideLeft:
		return layout.Rect{X: r.X, Y: r.Y, W: r.W / 2, H: r.H}
	case layout.SideRight:
		return layout.Rect{X: r.X + r.W/2, Y: r.Y, W: r.W - r.W/2, H: r.H}
	case layout.SideTop:
		return layout.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H / 2}
	case layout.SideBottom:
		return layout.Rect{X: r.X, Y: r.Y + r.H/2, W: r.W, H: r.H - r.H/2}
	default:
		return r
	}
}

// beginPaneDrag picks a pane up.
func (a *App) beginPaneDrag(src layout.PaneID) {
	if _, ok := a.rects[src]; !ok {
		return
	}
	if len(a.rects) < 2 {
		a.setStatus("there is nowhere to move the only pane")
		return
	}
	a.paneDrag = &paneDragState{src: src, target: src, side: layout.SideSwap}
}

// updatePaneDrag follows the pointer.
func (a *App) updatePaneDrag(x, y int) {
	if a.paneDrag == nil {
		return
	}
	id := paneAt(a.rects, x, y)
	if id == 0 {
		// Over a divider or the status bar: the last target is kept rather
		// than cleared, so crossing a divider does not flicker the preview.
		return
	}
	a.paneDrag.target = id
	a.paneDrag.side = dropZone(a.rects[id], x, y)
}

// finishPaneDrag drops the pane where the preview said it would go.
func (a *App) finishPaneDrag() {
	d := a.paneDrag
	a.paneDrag = nil
	a.clearNext = true
	if d == nil || d.target == d.src {
		return
	}
	root, err := layout.Move(a.root, d.src, d.target, d.side)
	if err != nil {
		a.setStatus("%s", err.Error())
		return
	}
	a.root = root
	a.layoutChanged = true
	a.relayout()
	a.setFocus(d.src)
}

// cancelPaneDrag puts the pane back.
func (a *App) cancelPaneDrag() {
	if a.paneDrag == nil {
		return
	}
	a.paneDrag = nil
	a.clearNext = true
}

// drawPaneDrag shows where the pane would land.
//
// The whole destination region is tinted rather than outlined: an outline on a
// pane full of text is hard to pick out, and the point of the preview is to be
// unmissable before you let go.
func (a *App) drawPaneDrag(scr uv.Screen) {
	d := a.paneDrag
	if d == nil || d.target == d.src {
		return
	}
	r, ok := a.rects[d.target]
	if !ok {
		return
	}
	area := previewRect(r, d.side)
	fill, edge, label := color.Color(dropFill), color.Color(dropEdge), d.side.String()
	if d.side == layout.SideSwap {
		fill, edge, label = dropSwapBg, dropSwapFg, "swap"
	}
	render.Fill(scr, uv.Rect(area.X, area.Y, area.W, area.H), fill)
	if area.H > 0 && area.W > 2 {
		render.Text(scr, area.X+1, area.Y+area.H/2, "▸ "+label, edge, fill)
	}
}
