package hologram

import (
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/holo"
	"claudecontrol/internal/render"
)

var bgPanel = color.RGBA{R: 0x06, G: 0x09, B: 0x12, A: 0xff}

// sphereRenderer draws the particle sphere.
type sphereRenderer struct {
	base   holo.Params
	sphere *holo.Sphere
	sig    Signal
	cols   int
	rows   int
}

func newSphere(p holo.Params) Renderer {
	return &sphereRenderer{base: p, sphere: holo.NewSphere(p)}
}

func (s *sphereRenderer) Resize(cols, rows int) {
	s.cols, s.rows = cols, rows
	w, h := holo.DotsFor(holo.ModeBraille, cols, rows)
	s.sphere.Resize(w, h)
}

func (s *sphereRenderer) Step(dt float64) { s.sphere.Step(dt) }

// SetSignal maps what the sessions are doing onto the animation.
//
// Rotation follows how many sessions are working, so a busy machine visibly
// turns faster. The mapping is deliberately gentle: the panel should read as
// alive, not as an alarm.
func (s *sphereRenderer) SetSignal(sig Signal) {
	s.sig = sig
	p := s.base
	p.Rotation = s.base.Rotation * (1 + 0.5*float64(sig.Active))
	s.sphere.SetParams(p)
}

func (s *sphereRenderer) Draw(scr uv.Screen, area uv.Rectangle) {
	render.Fill(scr, area, bgPanel)
	dots := s.sphere.Dots()
	w, h := holo.DotsFor(holo.ModeBraille, s.cols, s.rows)
	if len(dots) < w*h {
		return
	}
	tint := tintFor(s.sig.Worst)
	for cy := 0; cy < s.rows && area.Min.Y+cy < area.Max.Y; cy++ {
		for cx := 0; cx < s.cols && area.Min.X+cx < area.Max.X; cx++ {
			glyph, peak := holo.BrailleCell(dots, w, h, cx, cy, holo.Threshold)
			if glyph == 0x2800 {
				continue
			}
			r, g, b := Ramp(peak)
			cell := uv.EmptyCell
			cell.Content = string(glyph)
			cell.Style.Fg = tint(r, g, b)
			cell.Style.Bg = bgPanel
			scr.SetCell(area.Min.X+cx, area.Min.Y+cy, &cell)
		}
	}
}

// Ramp turns a dot's brightness into a colour: deep navy through blue and cyan
// to white. The gamma lift keeps faint trails visible instead of crushing them
// into the background.
func Ramp(v float32) (int, int, int) {
	if v > 1 {
		v = 1
	}
	if v < 0 {
		v = 0
	}
	g := float32(math.Pow(float64(v), 0.62))
	var r, gr, b float32
	if g < 0.55 {
		u := g / 0.55
		r, gr, b = 14+u*(28-14), 44+u*(150-44), 96+u*(228-96)
	} else {
		u := (g - 0.55) / 0.45
		r, gr, b = 28+u*(236-28), 150+u*(252-150), 228+u*(255-228)
	}
	return int(r), int(gr), int(b)
}

// tintFor shifts the palette by the worst state in the pool. A session waiting
// on you turns the sphere amber; one that died turns it red.
func tintFor(worst string) func(r, g, b int) color.Color {
	switch worst {
	case "waiting":
		return func(r, g, b int) color.Color {
			return color.RGBA{R: uint8(clamp(g)), G: uint8(clamp(g * 3 / 4)), B: uint8(clamp(r)), A: 0xff}
		}
	case "exited":
		return func(r, g, b int) color.Color {
			return color.RGBA{R: uint8(clamp(g)), G: uint8(clamp(r)), B: uint8(clamp(r)), A: 0xff}
		}
	default:
		return func(r, g, b int) color.Color {
			return color.RGBA{R: uint8(clamp(r)), G: uint8(clamp(g)), B: uint8(clamp(b)), A: 0xff}
		}
	}
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
