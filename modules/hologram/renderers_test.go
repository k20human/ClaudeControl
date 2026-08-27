package hologram

import (
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// dots counts the ring's own glyph.
func (g *grid) dots() int {
	n := 0
	for _, c := range g.cells {
		if c.Content == "·" {
			n++
		}
	}
	return n
}

// colours reports how many distinct foregrounds the ring used.
func (g *grid) colours() map[color.Color]int {
	out := map[color.Color]int{}
	for _, c := range g.cells {
		if c.Content == "·" {
			out[c.Style.Fg]++
		}
	}
	return out
}

// settle runs a renderer long enough for the eased mood to have arrived: the
// time constant is a second, so five is well past it.
func settle(r Renderer, seconds float64) {
	for t := 0.0; t < seconds; t += 1.0 / 30 {
		r.Step(1.0 / 30)
	}
}

func drawnRing(t *testing.T, sig Signal, seconds float64) *grid {
	t.Helper()
	r := newRing()
	r.Resize(40, 18)
	r.SetSignal(sig)
	settle(r, seconds)
	g := newGrid(40, 18)
	r.Draw(g, uv.Rect(0, 0, 40, 18))
	return g
}

// A dead session loses its coherence without the ring leaving: the same
// signature the sphere uses, said in the shapes the ring has.
func TestTheRingComesApartWhenASessionDies(t *testing.T) {
	alive := drawnRing(t, Signal{Worst: "working"}, 5)
	dead := drawnRing(t, Signal{Worst: "exited"}, 5)

	if dead.dots() >= alive.dots() {
		t.Errorf("a dead ring drew %d dots against %d alive; it should be coming apart",
			dead.dots(), alive.dots())
	}
	if dead.dots() == 0 {
		t.Error("the ring vanished entirely; it should scatter, not leave")
	}
}

// Waiting is swept end to end by a slow band. On a ring that is one bright arc
// on a dim one, which is more than one colour where every other state is one.
func TestTheRingIsSweptWhileWaiting(t *testing.T) {
	waiting := drawnRing(t, Signal{Worst: "waiting"}, 5)
	working := drawnRing(t, Signal{Worst: "working"}, 5)

	if n := len(waiting.colours()); n < 2 {
		t.Errorf("the waiting ring used %d colour(s); the band has to show", n)
	}
	if n := len(working.colours()); n != 1 {
		t.Errorf("the working ring used %d colours; the band belongs to waiting alone", n)
	}
}

// Resting breathes deeply, working shortens its breath and holds together.
// The measure is how far the ring's own width travels over several seconds.
func TestTheRingBreathesDeeplyAtRestAndShallowlyAtWork(t *testing.T) {
	beat := func(state string) int {
		lo, hi := 1<<30, -1
		for at := 5.0; at < 11; at += 0.1 {
			w := ringWidth(drawnRing(t, Signal{Worst: state}, at))
			if w < lo {
				lo = w
			}
			if w > hi {
				hi = w
			}
		}
		return hi - lo
	}
	idle, working := beat("idle"), beat("working")
	if idle <= working {
		t.Errorf("resting breathes across %d columns and working across %d; "+
			"resting should be the deeper of the two", idle, working)
	}
	if working == 0 {
		t.Error("the working ring does not beat at all")
	}
}

// ringWidth is the distance from the leftmost dot to the rightmost, which is
// the ring's diameter as drawn.
func ringWidth(g *grid) int {
	lo, hi := 1<<30, -1
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			if g.cells[y*g.w+x].Content != "·" {
				continue
			}
			if x < lo {
				lo = x
			}
			if x > hi {
				hi = x
			}
		}
	}
	if hi < 0 {
		return 0
	}
	return hi - lo
}

func drawnFace(t *testing.T, sig Signal, seconds float64) *grid {
	t.Helper()
	r := newAvatar()
	r.Resize(20, 9)
	r.SetSignal(sig)
	settle(r, seconds)
	g := newGrid(20, 9)
	r.Draw(g, uv.Rect(0, 0, 20, 9))
	return g
}

// A face that never blinks is the one thing that reads as dead, which is the
// opposite of what this panel is for.
func TestTheFaceBlinks(t *testing.T) {
	r := newAvatar()
	r.Resize(20, 9)
	r.SetSignal(Signal{Worst: "idle"})

	closed, open := 0, 0
	for i := 0; i < 400; i++ {
		settle(r, 1.0/30)
		g := newGrid(20, 9)
		r.Draw(g, uv.Rect(0, 0, 20, 9))
		shut := false
		for y := 0; y < 9; y++ {
			if strings.Contains(g.row(y), "‾") {
				shut = true
			}
		}
		if shut {
			closed++
		} else {
			open++
		}
	}
	if closed == 0 {
		t.Error("the face never blinked")
	}
	if open == 0 {
		t.Error("the face never opened its eyes")
	}
	// A blink is an instant, not a state: it must be much rarer than not.
	if closed > open/4 {
		t.Errorf("shut for %d frames of %d; a blink should be brief", closed, closed+open)
	}
}

// Waiting asks to be noticed, resting does not. The face says it by how far
// its glow travels.
func TestTheFaceGlowsHarderWhileWaiting(t *testing.T) {
	span := func(state string) int {
		lo, hi := 255, 0
		for at := 5.0; at < 10; at += 0.2 {
			g := drawnFace(t, Signal{Worst: state}, at)
			for _, c := range g.cells {
				if c.Content == " " || c.Content == "" {
					continue
				}
				r, _, _, _ := c.Style.Fg.RGBA()
				v := int(r >> 8)
				if v < lo {
					lo = v
				}
				if v > hi {
					hi = v
				}
			}
		}
		return hi - lo
	}
	waiting, idle := span("waiting"), span("idle")
	if waiting <= idle {
		t.Errorf("waiting glows across %d, resting across %d; waiting should be the one that asks", waiting, idle)
	}
}
