package hologram

import (
	"fmt"
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/render"
)

var ringFg = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}

// ringRenderer draws concentric arcs, each one a number rather than an
// ornament: it trades the sphere's presence for something readable at a
// glance.
type ringRenderer struct {
	sig        Signal
	t          float64
	cols, rows int
}

func newRing() Renderer { return &ringRenderer{} }

func (r *ringRenderer) Resize(cols, rows int) { r.cols, r.rows = cols, rows }
func (r *ringRenderer) Step(dt float64)       { r.t += dt }
func (r *ringRenderer) SetSignal(s Signal)    { r.sig = s }

func (r *ringRenderer) Draw(scr uv.Screen, area uv.Rectangle) {
	render.Fill(scr, area, bgPanel)
	if r.cols < 6 || r.rows < 4 {
		return
	}
	cx := float64(area.Min.X) + float64(r.cols)/2
	cy := float64(area.Min.Y) + float64(r.rows)/2

	// Cells are about twice as tall as wide, so the vertical radius is halved
	// to bring the circle back to round.
	radius := math.Min(float64(r.cols)/2, float64(r.rows))
	pulse := 0.85 + 0.15*math.Sin(r.t*1.6)

	for i := 0; i < 720; i++ {
		a := float64(i) / 720 * 2 * math.Pi
		x := int(cx + math.Cos(a)*radius*pulse)
		y := int(cy + math.Sin(a)*radius*pulse/2)
		if x < area.Min.X || x >= area.Max.X || y < area.Min.Y || y >= area.Max.Y {
			continue
		}
		cell := uv.EmptyCell
		cell.Content = "·"
		cell.Style.Fg = ringFg
		cell.Style.Bg = bgPanel
		scr.SetCell(x, y, &cell)
	}

	label := fmt.Sprintf("%d active", r.sig.Active)
	render.Text(scr, int(cx)-len(label)/2, int(cy), label, ringFg, bgPanel)
}
