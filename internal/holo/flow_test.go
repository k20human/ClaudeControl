package holo_test

import (
	"math"
	"testing"

	"claudecontrol/internal/holo"
)

// The velocity must be tangent to the sphere. A field with any radial
// component pushes particles off the surface, and normalising them back every
// frame turns that error into a slow drift towards the poles.
func TestFieldIsTangentToTheSphere(t *testing.T) {
	f := holo.NewField(7, 7)
	pts := []holo.Vec{
		{X: 1}, {Y: 1}, {Z: 1},
		holo.Vec{X: 1, Y: 1, Z: 1}.Unit(),
		holo.Vec{X: -0.3, Y: 0.5, Z: -0.8}.Unit(),
	}
	for _, p := range pts {
		v := f.At(p, 0)
		if radial := math.Abs(v.Dot(p)); radial > 1e-9 {
			t.Errorf("At(%+v) has a radial component of %g, want zero", p, radial)
		}
	}
}

// The field varies with time, or the sphere would be a frozen pattern that
// merely rotates.
func TestFieldChangesOverTime(t *testing.T) {
	f := holo.NewField(7, 7)
	p := holo.Vec{X: 0.3, Y: -0.5, Z: 0.8}.Unit()
	a, b := f.At(p, 0), f.At(p, 5)
	if a.Add(b.Scale(-1)).Len() < 1e-6 {
		t.Fatal("the field is identical five seconds apart")
	}
}

// The same seed must give the same field, or a golden-frame test could never
// be written for anything downstream.
func TestFieldIsReproducible(t *testing.T) {
	p := holo.Vec{X: 0.1, Y: 0.2, Z: 0.97}.Unit()
	a := holo.NewField(42, 7).At(p, 1.5)
	b := holo.NewField(42, 7).At(p, 1.5)
	if a != b {
		t.Fatalf("same seed gave %+v then %+v", a, b)
	}
}

func TestRotationsPreserveLength(t *testing.T) {
	v := holo.Vec{X: 0.3, Y: -0.4, Z: 0.86}.Unit()
	for _, a := range []float64{0.1, 1, math.Pi, 4} {
		if got := holo.RotateY(v, a).Len(); math.Abs(got-1) > 1e-12 {
			t.Errorf("RotateY length = %g, want 1", got)
		}
		if got := holo.RotateX(v, a).Len(); math.Abs(got-1) > 1e-12 {
			t.Errorf("RotateX length = %g, want 1", got)
		}
	}
}
