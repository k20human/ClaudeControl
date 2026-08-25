// Throwaway prototype: particle-sphere hologram rendered in a terminal.
//
// This is NOT part of the final architecture. It exists only to judge whether
// the terminal can carry the look before committing to a design.
//
// Keys: q / Ctrl+C quit | b braille <-> half-block | g glow | +/- particles
//
//	[ / ] speed     | , / . trail persistence  | r reseed
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"golang.org/x/term"
)

type vec3 struct{ X, Y, Z float64 }

func (a vec3) add(b vec3) vec3      { return vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a vec3) scale(s float64) vec3 { return vec3{a.X * s, a.Y * s, a.Z * s} }
func (a vec3) cross(b vec3) vec3 {
	return vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}
func (a vec3) unit() vec3 {
	l := math.Sqrt(a.X*a.X + a.Y*a.Y + a.Z*a.Z)
	if l == 0 {
		return vec3{0, 0, 1}
	}
	return a.scale(1 / l)
}

// wave is one term of a scalar stream function on the sphere. Velocity is
// grad(psi) x p: tangent to the unit sphere and divergence-free. That second
// property is what makes the filaments long and non-crossing rather than
// clumping into blobs.
type wave struct {
	k            vec3
	amp, phi, om float64
}

func newWaves(rng *rand.Rand, n int) []wave {
	ws := make([]wave, n)
	for i := range ws {
		dir := vec3{rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()}.unit()
		mag := 1.6 + rng.Float64()*2.6
		ws[i] = wave{
			k:   dir.scale(mag),
			amp: (0.5 + rng.Float64()) / mag,
			phi: rng.Float64() * 2 * math.Pi,
			om:  (rng.Float64()*2 - 1) * 0.22,
		}
	}
	return ws
}

func flow(p vec3, t float64, ws []wave) vec3 {
	var g vec3
	for i := range ws {
		w := &ws[i]
		c := math.Cos(w.k.X*p.X + w.k.Y*p.Y + w.k.Z*p.Z + w.phi + w.om*t)
		g = g.add(w.k.scale(w.amp * c))
	}
	return g.cross(p)
}

// Fibonacci lattice: even coverage with no visible seam, unlike lat/long.
func seedSphere(n int, rng *rand.Rand) []vec3 {
	pts := make([]vec3, n)
	ga := math.Pi * (3 - math.Sqrt(5))
	for i := 0; i < n; i++ {
		y := 1 - 2*(float64(i)+0.5)/float64(n)
		r := math.Sqrt(math.Max(0, 1-y*y))
		th := ga * float64(i)
		j := vec3{rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()}.scale(0.02)
		pts[i] = vec3{math.Cos(th) * r, y, math.Sin(th) * r}.add(j).unit()
	}
	return pts
}

// Braille cell U+2800 + mask. Bit layout is historical, not row-major.
var brailleBit = [4][2]byte{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

func splat(buf []float32, w, h int, fx, fy float64, v float32) {
	x0, y0 := int(math.Floor(fx)), int(math.Floor(fy))
	tx, ty := float32(fx-float64(x0)), float32(fy-float64(y0))
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			x, y := x0+dx, y0+dy
			if x < 0 || y < 0 || x >= w || y >= h {
				continue
			}
			wx, wy := tx, ty
			if dx == 0 {
				wx = 1 - tx
			}
			if dy == 0 {
				wy = 1 - ty
			}
			buf[y*w+x] += v * wx * wy
		}
	}
}

// Navy -> blue -> cyan -> white, gamma-lifted so faint trails stay visible.
func ramp(t float32) (int, int, int) {
	if t > 1 {
		t = 1
	}
	if t < 0 {
		t = 0
	}
	g := float32(math.Pow(float64(t), 0.62))
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

type writer struct {
	out            *bufio.Writer
	lastFg, lastBg int
}

func (w *writer) fg(r, g, b int) {
	c := r<<16 | g<<8 | b
	if c != w.lastFg {
		fmt.Fprintf(w.out, "\x1b[38;2;%d;%d;%dm", r, g, b)
		w.lastFg = c
	}
}

func (w *writer) bg(r, g, b int) {
	c := r<<16 | g<<8 | b
	if c != w.lastBg {
		fmt.Fprintf(w.out, "\x1b[48;2;%d;%d;%dm", r, g, b)
		w.lastBg = c
	}
}

func (w *writer) reset() {
	w.out.WriteString("\x1b[0m")
	w.lastFg, w.lastBg = -1, -1
}

func main() {
	var (
		nPart   = flag.Int("n", 0, "particle count (0 = auto from pane size)")
		speed   = flag.Float64("speed", 0.18, "flow speed")
		decay   = flag.Float64("decay", 0.90, "trail persistence per frame (0-1)")
		fps     = flag.Int("fps", 30, "target frames per second")
		glow    = flag.Float64("glow", 0.55, "interior glow, 0 disables")
		halfblk = flag.Bool("halfblock", false, "start in half-block mode instead of braille")
		dump    = flag.Int("dump", 0, "render N frames headless then print the last one and exit")
		dcols   = flag.Int("cols", 100, "columns when dumping")
		drows   = flag.Int("rows", 34, "rows when dumping")
	)
	flag.Parse()

	rng := rand.New(rand.NewSource(7))
	ws := newWaves(rng, 7)
	autoN := *nPart == 0
	pts, wts, spd := seedSphere(*nPart, rng), seedWeights(*nPart, rng), seedSpeeds(*nPart, rng)

	out := bufio.NewWriterSize(os.Stdout, 1<<21)
	w := &writer{out: out, lastFg: -1, lastBg: -1}
	braille := !*halfblk

	var buf []float32
	var cols, rows, dw, dh int
	resize := func(c, r int) {
		cols, rows = c, r
		dw, dh = c*2, r*4
		buf = make([]float32, dw*dh)
		if autoN {
			// One particle per ~9 dots of disc area keeps the shell sparse
			// enough to read as translucent at any pane size.
			rad := 0.44 * math.Min(float64(dw), float64(dh))
			n := int(math.Pi * rad * rad / 22)
			if n < 250 {
				n = 250
			}
			*nPart = n
			pts, wts, spd = seedSphere(n, rng), seedWeights(n, rng), seedSpeeds(n, rng)
		}
	}

	step := func(dt, t float64) {
		for i := range pts {
			v := flow(pts[i], t, ws)
			pts[i] = pts[i].add(v.scale(*speed * float64(spd[i]) * dt)).unit()
		}
	}

	// Deposit particles into the dot buffer after fading the previous frame.
	// The fade is what turns discrete points into filaments.
	deposit := func(t float64) {
		d := float32(*decay)
		for i := range buf {
			buf[i] *= d
		}
		ct, st := math.Cos(t*0.09), math.Sin(t*0.09)
		tilt := 0.35
		ctl, stl := math.Cos(tilt), math.Sin(tilt)
		cx, cy := float64(dw)/2, float64(dh)/2
		rad := 0.44 * math.Min(float64(dw), float64(dh))
		breath := float32(0.70 + 0.30*math.Sin(2*math.Pi*t/9))
		for i, p := range pts {
			// tilt about X, then spin about Y
			y1 := p.Y*ctl - p.Z*stl
			z1 := p.Y*stl + p.Z*ctl
			x2 := p.X*ct + z1*st
			z2 := -p.X*st + z1*ct
			depth := 0.5*z2 + 0.5 // 0 back, 1 front
			inten := breath * wts[i] * float32(0.20+0.80*depth*depth)
			splat(buf, dw, dh, cx+x2*rad, cy-y1*rad, inten)
		}
	}

	draw := func(hud string) {
		out.WriteString("\x1b[H")
		w.reset()
		cxd, cyd := float64(dw)/2, float64(dh)/2
		rad := 0.44 * math.Min(float64(dw), float64(dh))
		vrows := rows
		if hud != "" {
			vrows = rows - 1
		}
		for cy := 0; cy < vrows; cy++ {
			fmt.Fprintf(out, "\x1b[%d;1H", cy+1)
			for cx := 0; cx < cols; cx++ {
				gr, gg, gb := 0, 0, 0
				if *glow > 0 {
					ddx := (float64(cx)*2 + 1 - cxd) / rad
					ddy := (float64(cy)*4 + 2 - cyd) / rad
					d2 := ddx*ddx + ddy*ddy
					if d2 < 1 {
						f := math.Pow(1-d2, 1.4) * *glow
						gr, gg, gb = int(9*f), int(15*f), int(46*f)
					}
				}
				w.bg(gr, gg, gb)

				if braille {
					var mask byte
					var peak float32
					for dy := 0; dy < 4; dy++ {
						for dx := 0; dx < 2; dx++ {
							v := buf[(cy*4+dy)*dw+cx*2+dx]
							if v > 0.20 {
								mask |= brailleBit[dy][dx]
								if v > peak {
									peak = v
								}
							}
						}
					}
					if mask == 0 {
						w.out.WriteByte(' ')
						continue
					}
					w.fg(ramp(peak))
					w.out.WriteRune(rune(0x2800 + int(mask)))
				} else {
					px := func(py int) float32 {
						var s float32
						for dy := 0; dy < 2; dy++ {
							for dx := 0; dx < 2; dx++ {
								s += buf[(py*2+dy)*dw+cx*2+dx]
							}
						}
						return s / 4
					}
					up, lo := px(cy*2), px(cy*2+1)
					if up < 0.20 && lo < 0.20 {
						w.out.WriteByte(' ')
						continue
					}
					ur, ug, ub := ramp(up)
					lr, lg, lb := ramp(lo)
					if lo < 0.20 {
						lr, lg, lb = gr, gg, gb
					}
					w.fg(ur, ug, ub)
					w.bg(lr, lg, lb)
					w.out.WriteRune('▀')
				}
			}
		}
		if hud != "" {
			w.reset()
			fmt.Fprintf(out, "\x1b[%d;1H\x1b[38;2;90;110;140m%-*s", rows, cols, hud)
		}
		w.reset()
		out.Flush()
	}

	if *dump > 0 {
		resize(*dcols, *drows)
		dt := 1.0 / float64(*fps)
		for i := 0; i < *dump; i++ {
			step(dt, float64(i)*dt)
			deposit(float64(i) * dt)
		}
		out.WriteString("\x1b[2J")
		draw("")
		out.WriteString("\x1b[0m\n")
		out.Flush()
		return
	}

	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdin is not a terminal:", err)
		os.Exit(1)
	}
	defer term.Restore(fd, old)
	out.WriteString("\x1b[?1049h\x1b[?25l\x1b[2J")
	defer func() {
		out.WriteString("\x1b[0m\x1b[?25h\x1b[?1049l")
		out.Flush()
	}()

	keys := make(chan byte, 32)
	go func() {
		b := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(b)
			if err != nil || n == 0 {
				close(keys)
				return
			}
			keys <- b[0]
		}
	}()

	c0, r0, _ := term.GetSize(fd)
	if c0 < 20 {
		c0, r0 = 100, 34
	}
	resize(c0, r0)

	tick := time.NewTicker(time.Second / time.Duration(*fps))
	defer tick.Stop()
	dt := 1.0 / float64(*fps)
	t := 0.0
	frames, fpsShown := 0, 0
	last := time.Now()

	for {
		select {
		case k, ok := <-keys:
			if !ok || k == 'q' || k == 3 {
				return
			}
			switch k {
			case 'b':
				braille = !braille
			case 'g':
				if *glow > 0 {
					*glow = 0
				} else {
					*glow = 0.55
				}
			case '+', '=':
				autoN = false
				*nPart = int(float64(*nPart) * 1.35)
				pts, wts, spd = seedSphere(*nPart, rng), seedWeights(*nPart, rng), seedSpeeds(*nPart, rng)
			case '-', '_':
				autoN = false
				if *nPart > 400 {
					*nPart = int(float64(*nPart) / 1.35)
					pts, wts, spd = seedSphere(*nPart, rng), seedWeights(*nPart, rng), seedSpeeds(*nPart, rng)
				}
			case ']':
				*speed *= 1.25
			case '[':
				*speed /= 1.25
			case '.':
				*decay = math.Min(0.985, *decay+0.01)
			case ',':
				*decay = math.Max(0.5, *decay-0.01)
			case 'r':
				ws = newWaves(rng, 7)
				pts, wts, spd = seedSphere(*nPart, rng), seedWeights(*nPart, rng), seedSpeeds(*nPart, rng)
				for i := range buf {
					buf[i] = 0
				}
			}
			continue
		case <-tick.C:
		}

		if c, r, err := term.GetSize(fd); err == nil && (c != cols || r != rows) && c > 20 && r > 8 {
			resize(c, r)
			out.WriteString("\x1b[2J")
		}

		t += dt
		step(dt, t)
		deposit(t)

		frames++
		if el := time.Since(last); el >= time.Second {
			fpsShown = int(float64(frames) / el.Seconds())
			frames, last = 0, time.Now()
		}
		mode := "braille"
		if !braille {
			mode = "half-block"
		}
		draw(fmt.Sprintf(" %s  %dx%d cells  %d particles  speed %.2f  trail %.2f  %d fps   [b]mode [g]glow [+/-]particles [ ]speed [,.]trail [r]reseed [q]quit",
			mode, cols, rows, *nPart, *speed, *decay, fpsShown))
	}
}

// Most particles are faint; a few are bright. Cubing the uniform draw scatters
// vivid filaments over a dim haze instead of producing a uniform shell.
func seedWeights(n int, rng *rand.Rand) []float32 {
	w := make([]float32, n)
	for i := range w {
		u := rng.Float64()
		w[i] = float32(0.10 + 1.05*u*u*u)
	}
	return w
}

// Per-particle speed. Squaring the draw keeps most particles nearly still and
// lets a handful dart across — the difference between a swarm and a mind
// following one thread at a time.
func seedSpeeds(n int, rng *rand.Rand) []float32 {
	s := make([]float32, n)
	for i := range s {
		u := rng.Float64()
		s[i] = float32(0.18 + 2.1*u*u)
	}
	return s
}
