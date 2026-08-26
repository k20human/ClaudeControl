package holo_test

import (
	"math"
	"testing"

	"claudecontrol/internal/holo"
)

// Density follows the area of the pane. A fixed particle count saturates a
// small pane and leaves a large one empty — the defect the throwaway prototype
// exposed on its first frame.
func TestParticleCountFollowsTheArea(t *testing.T) {
	s := holo.NewSphere(holo.DefaultParams())

	s.Resize(100, 100)
	small := s.Count()
	s.Resize(300, 300)
	large := s.Count()

	if small == 0 || large == 0 {
		t.Fatalf("counts are %d and %d; a sized sphere must hold particles", small, large)
	}
	if large <= small*4 {
		t.Errorf("nine times the area gave %d against %d particles; density is not following the area", large, small)
	}
}

// Deposits add, and the decaying buffer bounds the sum: roughly one deposit
// divided by one minus the trail. The test is that it converges — an
// unbounded sum would wash the sphere out to a solid disc within seconds.
func TestDotsStayBounded(t *testing.T) {
	s := holo.NewSphere(holo.DefaultParams())
	s.Resize(120, 120)

	for i := 0; i < 400; i++ {
		s.Step(1.0 / 30)
	}
	early := brightest(s.Dots())
	for i := 0; i < 1600; i++ {
		s.Step(1.0 / 30)
	}
	late := brightest(s.Dots())

	// 1.15 is the brightest a single particle can be; 0.90 the trail.
	const ceiling = 1.15 / (1 - 0.90) * 1.5
	if late > ceiling {
		t.Fatalf("brightest dot reached %g after two thousand frames, past the %g the decay should bound it to", late, ceiling)
	}
	if late > early*3 {
		t.Fatalf("brightest dot went from %g to %g; it is not converging", early, late)
	}
}

// Enough of the sphere must actually clear the drawing threshold, or the panel
// shows a scattering of dots rather than a sphere.
func TestEnoughDotsClearTheThreshold(t *testing.T) {
	s := holo.NewSphere(holo.DefaultParams())
	s.Resize(120, 120)
	for i := 0; i < 300; i++ {
		s.Step(1.0 / 30)
	}
	above := 0
	for _, v := range s.Dots() {
		if v > holo.Threshold {
			above++
		}
	}
	if above < 500 {
		t.Fatalf("only %d dots clear the threshold; the sphere would barely be visible", above)
	}
}

// Trails fade. Without decay the sphere fills in and never empties again.
func TestDotsFadeWhenNothingIsDeposited(t *testing.T) {
	p := holo.DefaultParams()
	p.Speed = 0 // particles stay put, so only decay acts
	s := holo.NewSphere(p)
	s.Resize(120, 120)
	for i := 0; i < 60; i++ {
		s.Step(1.0 / 30)
	}

	before := brightest(s.Dots())
	p.Density = 0 // nothing more is deposited
	s.SetParams(p)
	s.Resize(120, 120)
	for i := 0; i < 60; i++ {
		s.Step(1.0 / 30)
	}
	if after := brightest(s.Dots()); after >= before {
		t.Fatalf("brightest dot went from %g to %g; trails are not fading", before, after)
	}
}

// The sphere is round: nothing is deposited outside the disc it projects to.
func TestNothingIsDepositedOutsideTheDisc(t *testing.T) {
	const w, h = 120, 120
	s := holo.NewSphere(holo.DefaultParams())
	s.Resize(w, h)
	for i := 0; i < 200; i++ {
		s.Step(1.0 / 30)
	}
	dots := s.Dots()
	cx, cy := float64(w)/2, float64(h)/2
	radius := 0.46 * float64(minOf(w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if dots[y*w+x] <= 0 {
				continue
			}
			dx, dy := float64(x)-cx, float64(y)-cy
			if dx*dx+dy*dy > radius*radius {
				t.Fatalf("dot lit at (%d,%d), outside the projected disc", x, y)
			}
		}
	}
}

func brightest(dots []float32) float32 {
	var m float32
	for _, v := range dots {
		if v > m {
			m = v
		}
	}
	return m
}

func minOf(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// brightRow is the brightness-weighted row of the dot grid: where the light
// sits, in one number.
func brightRow(dots []float32, w, h int) float64 {
	var sum, weighted float64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := float64(dots[y*w+x])
			sum += v
			weighted += v * float64(y)
		}
	}
	if sum == 0 {
		return 0
	}
	return weighted / sum
}

// sweep runs a sphere and reports how far the light travelled up and down.
func sweep(t *testing.T, p holo.Params, w, h, frames int) float64 {
	t.Helper()
	s := holo.NewSphere(p)
	s.Resize(w, h)
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := 0; i < frames; i++ {
		s.Step(1.0 / 30)
		if i%15 != 0 {
			continue
		}
		r := brightRow(s.Dots(), w, h)
		lo, hi = math.Min(lo, r), math.Max(hi, r)
	}
	return hi - lo
}

// The sweeping band is what reads as waiting rather than working: nothing is
// carried anywhere, the surface is merely swept. Measured over six seconds,
// the light must travel far enough up and down to be seen as a band rather
// than as the sphere's own wobble.
func TestAPulseSweepsABandAcrossTheSphere(t *testing.T) {
	const w, h, frames = 60, 60, 180
	base := holo.DefaultParams()
	still := sweep(t, base, w, h, frames)

	pulsed := base
	pulsed.Pulse = 1
	moving := sweep(t, pulsed, w, h, frames)

	if moving < 3*still {
		t.Errorf("the light travels %.1f rows with a pulse and %.1f without; "+
			"the band is not distinguishable from the sphere's own wobble", moving, still)
	}
}

// Scatter breaks the filaments without letting anything leave the sphere: the
// particles are still constrained to the surface, they simply stop agreeing
// about where to go.
func TestScatterBreaksTheFilamentsWithoutLeavingTheSphere(t *testing.T) {
	const w, h, frames = 60, 60, 120
	base := holo.DefaultParams()

	still := holo.NewSphere(base)
	still.Resize(w, h)
	loose := base
	loose.Scatter = 1
	broken := holo.NewSphere(loose)
	broken.Resize(w, h)
	for i := 0; i < frames; i++ {
		still.Step(1.0 / 30)
		broken.Step(1.0 / 30)
	}

	var diff float64
	for i, v := range still.Dots() {
		d := float64(v - broken.Dots()[i])
		diff += d * d
	}
	if diff < 10 {
		t.Errorf("scatter changed the deposits by only %.2f; it is doing nothing", diff)
	}

	// Still a sphere: every lit dot inside the projected disc, and the sphere
	// has not gone dark.
	cx, cy := float64(w)/2, float64(h)/2
	limit := 0.46*float64(w) + 1.5
	lit := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if broken.Dots()[y*w+x] < holo.Threshold {
				continue
			}
			lit++
			dx, dy := float64(x)-cx, float64(y)-cy
			if math.Hypot(dx, dy) > limit {
				t.Fatalf("a dot is lit at (%d,%d), %.1f from the centre; the limit is %.1f",
					x, y, math.Hypot(dx, dy), limit)
			}
		}
	}
	if lit < 50 {
		t.Errorf("only %d dots are lit; scatter dissolved the sphere instead of stirring it", lit)
	}
}

// A spark is the visible sign of something having happened. Nothing emits one
// on its own, so a still sphere means a still session — and each one has to
// brighten the sphere while it lives, then be gone.
func TestASparkBrightensTheSphereThenLeaves(t *testing.T) {
	const w, h = 60, 60
	p := holo.DefaultParams()
	// No trail, so the measurement is of this frame and not of its history.
	p.Trail = 0

	quiet := holo.NewSphere(p)
	quiet.Resize(w, h)
	loud := holo.NewSphere(p)
	loud.Resize(w, h)
	loud.Emit(8)
	if loud.Sparks() != 8 {
		t.Fatalf("%d sparks in flight, want 8", loud.Sparks())
	}

	quiet.Step(1.0 / 30)
	loud.Step(1.0 / 30)
	if total(loud.Dots()) <= total(quiet.Dots()) {
		t.Errorf("eight sparks left the sphere no brighter: %.1f against %.1f",
			total(loud.Dots()), total(quiet.Dots()))
	}

	// They are short-lived by design; two seconds is longer than any of them.
	for i := 0; i < 60; i++ {
		loud.Step(1.0 / 30)
	}
	if loud.Sparks() != 0 {
		t.Errorf("%d sparks still in flight after two seconds", loud.Sparks())
	}
}

// A burst larger than the sphere can show must not paint it solid: the point
// of a trace is that it is noticeable.
func TestSparksAreBounded(t *testing.T) {
	s := holo.NewSphere(holo.DefaultParams())
	s.Resize(40, 40)
	s.Emit(500)
	if s.Sparks() > holo.MaxSparks {
		t.Errorf("%d sparks in flight, more than the %d allowed", s.Sparks(), holo.MaxSparks)
	}
}

func total(dots []float32) float64 {
	var sum float64
	for _, v := range dots {
		sum += float64(v)
	}
	return sum
}
