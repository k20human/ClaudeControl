package holo

import (
	"math"
	"math/rand"
)

// DotsPerParticle is how much of the projected disc each particle is given.
// One for twenty-two dots is the density the user settled on by eye; denser
// reads as a solid shell, sparser stops looking like a sphere.
const DotsPerParticle = 22

// Params are the knobs the settings menu will expose.
type Params struct {
	Speed    float64 // flow speed
	Trail    float64 // how much of a dot survives each frame
	Density  float64 // multiplier over the automatic particle count
	Rotation float64 // radians per second
	Breath   float64 // seconds for one brightness cycle, 0 to disable
	Seed     int64
}

// DefaultParams are the values validated by eye on 2026-08-25. They are
// recorded in proto/hologram/VALIDATED.md and should not drift without someone
// looking at the result again.
func DefaultParams() Params {
	return Params{
		Speed:    0.18,
		Trail:    0.90,
		Density:  1,
		Rotation: 0.09,
		Breath:   9,
		Seed:     7,
	}
}

// Sphere is a field of particles drifting over a sphere, deposited into a grid
// of dots.
type Sphere struct {
	p     Params
	field *Field

	pos     []Vec
	weight  []float32
	speedOf []float32

	w, h  int
	dots  []float32
	t     float64
	count int
}

// NewSphere builds a sphere. It holds no particles until it is sized.
func NewSphere(p Params) *Sphere {
	return &Sphere{p: p, field: NewField(p.Seed, 7)}
}

// SetParams replaces the parameters. A changed seed rebuilds the flow.
func (s *Sphere) SetParams(p Params) {
	if p.Seed != s.p.Seed {
		s.field = NewField(p.Seed, 7)
	}
	s.p = p
	s.reseed()
}

// Resize sets the dot grid and reseeds the particles to match its area.
func (s *Sphere) Resize(w, h int) {
	if w < 1 || h < 1 {
		return
	}
	s.w, s.h = w, h
	s.dots = make([]float32, w*h)
	s.reseed()
}

// Count is how many particles the sphere currently holds.
func (s *Sphere) Count() int { return s.count }

// Dots is the deposit grid, row by row.
func (s *Sphere) Dots() []float32 { return s.dots }

// radius is the projected disc's radius in dots.
func (s *Sphere) radius() float64 {
	m := s.w
	if s.h < m {
		m = s.h
	}
	return 0.44 * float64(m)
}

// reseed rebuilds the particles for the current size and density.
func (s *Sphere) reseed() {
	if s.w < 1 || s.h < 1 {
		return
	}
	r := s.radius()
	n := int(math.Pi * r * r / DotsPerParticle * s.p.Density)
	if n < 0 {
		n = 0
	}
	s.count = n

	rng := rand.New(rand.NewSource(s.p.Seed))
	s.pos = make([]Vec, n)
	s.weight = make([]float32, n)
	s.speedOf = make([]float32, n)

	// A Fibonacci lattice covers the sphere evenly with no visible seam, which
	// a latitude/longitude grid cannot do. The jitter keeps the first frames
	// from looking like a lattice before the flow has stirred them.
	ga := math.Pi * (3 - math.Sqrt(5))
	for i := 0; i < n; i++ {
		y := 1 - 2*(float64(i)+0.5)/float64(n)
		rad := math.Sqrt(math.Max(0, 1-y*y))
		th := ga * float64(i)
		j := Vec{rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()}.Scale(0.02)
		s.pos[i] = Vec{math.Cos(th) * rad, y, math.Sin(th) * rad}.Add(j).Unit()

		// Cubing a uniform draw leaves most particles faint and slow with a
		// handful vivid and quick. That contrast is what reads as a mind
		// following one thread rather than a swarm.
		u := rng.Float64()
		s.weight[i] = float32(0.10 + 1.05*u*u*u)
		v := rng.Float64()
		s.speedOf[i] = float32(0.18 + 2.1*v*v)
	}
}

// Step advances the simulation by dt seconds and deposits the particles.
func (s *Sphere) Step(dt float64) {
	if s.w < 1 || s.h < 1 {
		return
	}
	s.t += dt

	for i := range s.pos {
		v := s.field.At(s.pos[i], s.t)
		s.pos[i] = s.pos[i].Add(v.Scale(s.p.Speed * float64(s.speedOf[i]) * dt)).Unit()
	}

	trail := float32(s.p.Trail)
	for i := range s.dots {
		s.dots[i] *= trail
	}

	breath := float32(1)
	if s.p.Breath > 0 {
		breath = float32(0.70 + 0.30*math.Sin(2*math.Pi*s.t/s.p.Breath))
	}

	cx, cy := float64(s.w)/2, float64(s.h)/2
	r := s.radius()
	spin := s.t * s.p.Rotation
	const tilt = 0.35

	for i, p := range s.pos {
		q := RotateY(RotateX(p, tilt), spin)
		depth := 0.5*q.Z + 0.5 // 0 at the back, 1 at the front
		inten := breath * s.weight[i] * float32(0.20+0.80*depth*depth)
		s.splat(cx+q.X*r, cy-q.Y*r, inten)
	}
}

// splat deposits one particle across the four dots it falls between.
//
// The deposit adds. Brightness therefore builds where filaments cross, which
// is what gives the sphere its bright limb and its vivid intersections — with
// a decaying buffer the sum is bounded at roughly one deposit over one minus
// the trail, so it converges rather than running away.
//
// Taking a maximum instead was tried and is wrong: a dot then holds only the
// brightest single particle, every value lands under the threshold, and the
// sphere all but disappears. Two hundred frames at any size leave a handful of
// lit cells. What actually stopped the first prototype saturating was raising
// the threshold and lowering the density, not changing how deposits combine.
func (s *Sphere) splat(fx, fy float64, v float32) {
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	tx, ty := float32(fx-float64(x0)), float32(fy-float64(y0))
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			x, y := x0+dx, y0+dy
			if x < 0 || y < 0 || x >= s.w || y >= s.h {
				continue
			}
			wx, wy := tx, ty
			if dx == 0 {
				wx = 1 - tx
			}
			if dy == 0 {
				wy = 1 - ty
			}
			s.dots[y*s.w+x] += v * wx * wy
		}
	}
}
