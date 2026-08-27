package hologram

import (
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/holo"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/render"
)

// bgPanel is the ground every hologram renderer paints first. Black rather
// than the near-black blue it started as: on a screen beside a terminal of
// any other colour the blue read as a tint rather than as depth, and what the
// sphere hangs in should look like nothing at all.
var bgPanel = color.RGBA{A: 0xff}

// sphereRenderer draws the particle sphere.
type sphereRenderer struct {
	base   holo.Params
	sphere *holo.Sphere
	sig    Signal
	cols   int
	rows   int

	// cur eases towards want, so a change of state is something you watch
	// happen rather than something that has already happened.
	cur  mood
	want mood
}

func newSphere(p holo.Params) Renderer {
	start := moodFor(pool.StateIdle.String())
	return &sphereRenderer{
		base:   p,
		sphere: holo.NewSphere(start.apply(p)),
		cur:    start,
		want:   start,
	}
}

func (s *sphereRenderer) Resize(cols, rows int) {
	s.cols, s.rows = cols, rows
	w, h := holo.DotsFor(holo.ModeBraille, cols, rows)
	s.sphere.Resize(w, h)
}

func (s *sphereRenderer) Step(dt float64) {
	s.cur = s.cur.ease(s.want, dt)
	s.sphere.SetParams(s.cur.apply(s.base))
	s.sphere.Step(dt)
}

// Emit shows a burst: a handful of sparks running the sphere. Nothing calls
// this on a timer, so the panel is quiet exactly when the sessions are.
func (s *sphereRenderer) Emit(n int) { s.sphere.Emit(n) }

// Params returns the parameters as configured, before any signal is applied.
func (s *sphereRenderer) Params() holo.Params { return s.base }

// SetParams replaces them and re-applies the current signal, so a slider takes
// effect without waiting for the next pool announcement.
func (s *sphereRenderer) SetParams(p holo.Params) {
	s.base = p
	s.SetSignal(s.sig)
}

// SetSignal maps what the sessions are doing onto the animation.
//
// The state chooses the mood — how the sphere moves at all — and the number of
// working sessions then adds to its rotation, so a busy machine turns faster
// within the same character. Nothing is applied here: Step eases towards it,
// because a mood that arrived instantly would read as a cut rather than as a
// change of mind.
func (s *sphereRenderer) SetSignal(sig Signal) {
	s.sig = sig
	m := moodFor(sig.Worst)
	m.rotation *= 1 + 0.4*float64(sig.Active)
	s.want = m
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
