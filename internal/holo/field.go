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

	// Scatter blends a second, finer flow over the first. At zero the
	// particles stream in long filaments — a mind following one thread. Turned
	// up they break into eddies and the structure loses its coherence, which
	// is what a session that has ended should look like.
	//
	// Zero is the value that reproduces the field as it was first validated,
	// so a Params built without thinking about it behaves as before.
	Scatter float64

	// Pulse is the amplitude of a band of brightness sweeping from pole to
	// pole. Zero disables it. It is the one motion that reads as waiting
	// rather than working: nothing is carried anywhere, the surface is merely
	// swept.
	Pulse float64

	Seed int64
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

// PulsePeriod is how long the sweeping band takes to cross the sphere once.
// Slow on purpose: a fast sweep reads as a machine scanning, a slow one as
// something holding still and breathing.
const PulsePeriod = 4.0

// Sphere is a field of particles drifting over a sphere, deposited into a grid
// of dots.
type Sphere struct {
	p     Params
	field *Field
	fine  *Field

	pos     []Vec
	weight  []float32
	speedOf []float32

	w, h  int
	dots  []float32
	t     float64
	count int

	sparks   []spark
	sparkRNG *rand.Rand
}

// spark is a bright point running along a great circle and fading out. It is
// the visible sign of something having happened — a turn taken, a burst of
// thinking — and nothing emits one on its own, so a still sphere means a
// still session rather than an animation that happens to be paused.
type spark struct {
	u, v      Vec // an orthonormal pair spanning the circle
	angle     float64
	speed     float64
	age, life float64
}

// NewSphere builds a sphere. It holds no particles until it is sized.
func NewSphere(p Params) *Sphere {
	return &Sphere{
		p:     p,
		field: NewField(p.Seed, 7),
		// More waves from a different seed: the same construction, so it is
		// still tangent and still divergence-free, but at a finer scale.
		fine: NewField(p.Seed+9973, 17),
		// Its own generator, so emitting sparks cannot disturb the particle
		// layout — which is seeded for reproducibility.
		sparkRNG: rand.New(rand.NewSource(p.Seed + 31)),
	}
}

// MaxSparks bounds how many can be in flight. A burst that outran the fade
// would paint the sphere solid, and the point is that a trace is noticeable.
const MaxSparks = 24

// Emit sends n sparks along fresh great circles.
func (s *Sphere) Emit(n int) {
	for i := 0; i < n && len(s.sparks) < MaxSparks; i++ {
		// Any unit vector as the pole of the circle, then any two orthogonal
		// directions in the plane it defines.
		axis := Vec{s.sparkRNG.NormFloat64(), s.sparkRNG.NormFloat64(), s.sparkRNG.NormFloat64()}.Unit()
		u := axis.Cross(Vec{0, 1, 0})
		if u.Len() < 1e-3 {
			u = axis.Cross(Vec{1, 0, 0})
		}
		u = u.Unit()
		s.sparks = append(s.sparks, spark{
			u:     u,
			v:     axis.Cross(u).Unit(),
			angle: s.sparkRNG.Float64() * 2 * math.Pi,
			speed: 1.8 + s.sparkRNG.Float64()*2.2,
			life:  0.9 + s.sparkRNG.Float64()*0.9,
		})
	}
}

// Sparks is how many are in flight.
func (s *Sphere) Sparks() int { return len(s.sparks) }

// SetParams replaces the parameters. A changed seed rebuilds the flow.
//
// The particles are rebuilt only when the seed or the density changes, because
// rebuilding them puts every one back on the lattice it started from. That is
// right when the population itself changes and wrong for anything else: a
// caller easing the speed frame by frame would otherwise reset the animation
// sixty times a second.
func (s *Sphere) SetParams(p Params) {
	rebuild := p.Seed != s.p.Seed || p.Density != s.p.Density
	if p.Seed != s.p.Seed {
		s.field = NewField(p.Seed, 7)
		s.fine = NewField(p.Seed+9973, 17)
	}
	s.p = p
	if rebuild {
		s.reseed()
	}
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

	scatter := clamp01(s.p.Scatter)
	for i := range s.pos {
		v := s.field.At(s.pos[i], s.t)
		if scatter > 0 {
			f := s.fine.At(s.pos[i], s.t)
			v = v.Scale(1 - scatter).Add(f.Scale(scatter))
		}
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

	// The sweeping band, as a height on the tilted sphere. It runs from below
	// the south pole to above the north so that the band leaves the surface
	// entirely between passes rather than bouncing at the edges.
	pulse := clamp01(s.p.Pulse)
	band := math.Mod(s.t/PulsePeriod, 1)*2.6 - 1.3

	for i, p := range s.pos {
		q := RotateY(RotateX(p, tilt), spin)
		depth := 0.5*q.Z + 0.5 // 0 at the back, 1 at the front
		inten := breath * s.weight[i] * float32(0.20+0.80*depth*depth)
		if pulse > 0 {
			d := q.Y - band
			inten *= float32(1 + pulse*2.5*math.Exp(-d*d*20))
		}
		s.splat(cx+q.X*r, cy-q.Y*r, inten)
	}

	s.stepSparks(dt, cx, cy, r, tilt, spin)
}

// sparkTail is how many samples behind a spark are drawn. Enough to read as a
// streak, few enough that a spark is a line and not a smear.
const sparkTail = 7

// stepSparks advances the sparks and draws each as a short streak.
func (s *Sphere) stepSparks(dt, cx, cy, r, tilt, spin float64) {
	live := s.sparks[:0]
	for _, sp := range s.sparks {
		sp.angle += sp.speed * dt
		sp.age += dt
		if sp.age >= sp.life {
			continue
		}
		// Bright at birth, gone at the end of its life.
		head := float32(2.6 * (1 - sp.age/sp.life))
		for k := 0; k < sparkTail; k++ {
			a := sp.angle - float64(k)*0.05
			p := sp.u.Scale(math.Cos(a)).Add(sp.v.Scale(math.Sin(a)))
			q := RotateY(RotateX(p, tilt), spin)
			// Behind the sphere the streak is occluded, so it dims rather than
			// showing through.
			depth := 0.5*q.Z + 0.5
			fade := head * float32(1-float64(k)/sparkTail) * float32(0.15+0.85*depth)
			s.splat(cx+q.X*r, cy-q.Y*r, fade)
		}
		live = append(live, sp)
	}
	s.sparks = live
}

// clamp01 confines a knob to the range it is documented over, so a value
// arriving from a slider or a hand-edited file cannot invert the effect.
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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
