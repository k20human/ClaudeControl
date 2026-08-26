package holo_test

import (
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

// Deposit takes the brightest particle to cross a dot, never the sum. Adding
// saturates the moment a dot is crossed twice, which is what turned the
// prototype's first sphere into a solid disc.
func TestDotsNeverExceedTheBrightestParticle(t *testing.T) {
	s := holo.NewSphere(holo.DefaultParams())
	s.Resize(120, 120)
	for i := 0; i < 400; i++ {
		s.Step(1.0 / 30)
	}
	for i, v := range s.Dots() {
		if v > 1.2 {
			t.Fatalf("dot %d reached %g; deposit is accumulating rather than taking a maximum", i, v)
		}
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
