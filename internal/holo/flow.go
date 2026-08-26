package holo

import (
	"math"
	"math/rand"
)

// wave is one term of a scalar stream function on the sphere.
type wave struct {
	k            Vec
	amp, phi, om float64
}

// Field is a flow that carries particles across the surface of a sphere.
//
// The velocity is grad(psi) x p, where psi is a scalar stream function. That
// construction guarantees two things at once: the velocity is perpendicular to
// the radius, so particles stay on the sphere; and it is divergence-free, so
// they neither pile up nor thin out. Filaments come out long and separate
// instead of collapsing into blobs — which is what the reference footage
// shows, and what a naive random field does not produce.
type Field struct{ waves []wave }

// NewField builds a reproducible field from a seed.
func NewField(seed int64, waves int) *Field {
	rng := rand.New(rand.NewSource(seed))
	f := &Field{waves: make([]wave, waves)}
	for i := range f.waves {
		dir := Vec{rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()}.Unit()
		mag := 1.6 + rng.Float64()*2.6
		f.waves[i] = wave{
			k:   dir.Scale(mag),
			amp: (0.5 + rng.Float64()) / mag,
			phi: rng.Float64() * 2 * math.Pi,
			om:  (rng.Float64()*2 - 1) * 0.22,
		}
	}
	return f
}

// At returns the velocity at a point on the unit sphere.
func (f *Field) At(p Vec, t float64) Vec {
	var g Vec
	for i := range f.waves {
		w := &f.waves[i]
		c := math.Cos(w.k.Dot(p) + w.phi + w.om*t)
		g = g.Add(w.k.Scale(w.amp * c))
	}
	return g.Cross(p)
}
