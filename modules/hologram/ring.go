package hologram

import (
	"fmt"
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/render"
)

var (
	ringFg  = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	ringDim = color.RGBA{R: 0x1e, G: 0x3f, B: 0x46, A: 0xff}
)

// wobble is a cheap deterministic value in [0,1) for an angle. Not random: the
// gaps in a scattered ring have to stay with the part of the ring they belong
// to as it turns, and a random number would make them flicker in place.
func wobble(x float64) float64 {
	_, f := math.Modf(math.Sin(x) * 43758.5453)
	if f < 0 {
		f += 1
	}
	return f
}

// mix blends two colours, k running from 0 (a) to 1 (b).
func mix(a, b color.RGBA, k float64) color.RGBA {
	if k < 0 {
		k = 0
	}
	if k > 1 {
		k = 1
	}
	lerp := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*k) }
	return color.RGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: 0xff}
}

// ringRenderer draws a ring whose label is a number rather than an ornament:
// it trades the sphere's presence for something readable at a glance.
//
// It moves on the same vocabulary as the sphere — the mood table — so a state
// looks like itself whichever renderer is on screen. Working turns quickly and
// breathes shallowly, waiting is swept by a slow band, exited comes apart.
type ringRenderer struct {
	sig        Signal
	t          float64
	phase      float64 // how far the ring has turned, in radians
	cols, rows int

	cur, want mood
}

func newRing() Renderer {
	start := moodFor("idle")
	return &ringRenderer{cur: start, want: start}
}

func (r *ringRenderer) Resize(cols, rows int) { r.cols, r.rows = cols, rows }

func (r *ringRenderer) Step(dt float64) {
	r.cur = r.cur.ease(r.want, dt)
	r.t += dt * r.cur.speed
	r.phase += dt * r.cur.rotation
}

// SetSignal chooses the mood, and lets a busy machine turn faster inside it —
// the same rule the sphere follows, for the same reason.
func (r *ringRenderer) SetSignal(s Signal) {
	r.sig = s
	m := moodFor(s.Worst)
	m.rotation *= 1 + 0.4*float64(s.Active)
	r.want = m
}

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

	// Breath is the depth of the ring's own beat: deep at rest, shallow while
	// something is being worked on.
	depth := 0.15 * r.cur.breath
	if depth > 0.35 {
		depth = 0.35
	}
	beat := (1 - depth) + depth*math.Sin(r.t*1.6)

	// The band is the waiting state made visible: one bright arc travelling
	// round a ring that is otherwise still. It carries nothing anywhere, which
	// is the point — the work is yours now.
	band := math.Mod(r.t*0.9, 2*math.Pi)

	for i := 0; i < 720; i++ {
		a := float64(i)/720*2*math.Pi + r.phase
		// Scatter breaks the ring rather than moving it: a structure losing
		// its coherence without leaving. Deterministic in the angle, so the
		// gaps turn with the ring instead of flickering in place.
		if r.cur.scatter > 0 {
			if wobble(a*7.3) < r.cur.scatter {
				continue
			}
		}
		x := int(cx + math.Cos(a)*radius*beat)
		y := int(cy + math.Sin(a)*radius*beat/2)
		if x < area.Min.X || x >= area.Max.X || y < area.Min.Y || y >= area.Max.Y {
			continue
		}
		fg := ringFg
		if r.cur.pulse > 0 {
			// Distance round the circle to the band, the short way.
			d := math.Abs(math.Mod(a-band+3*math.Pi, 2*math.Pi) - math.Pi)
			if lit := 1 - d/0.6; lit > 0 {
				fg = mix(ringDim, ringFg, lit*r.cur.pulse)
			} else {
				fg = ringDim
			}
		}
		cell := uv.EmptyCell
		cell.Content = "·"
		cell.Style.Fg = fg
		cell.Style.Bg = bgPanel
		scr.SetCell(x, y, &cell)
	}

	label := fmt.Sprintf("%d active", r.sig.Active)
	render.Text(scr, int(cx)-len(label)/2, int(cy), label, ringFg, bgPanel)
}
