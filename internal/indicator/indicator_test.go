package indicator_test

import (
	"testing"
	"time"

	"claudecontrol/internal/indicator"
	"claudecontrol/internal/pool"
)

// A shape that moves means work is happening. If every state moved, motion
// would mean nothing.
func TestOnlyWorkTurns(t *testing.T) {
	base := time.Unix(0, 0)
	for _, s := range []pool.State{pool.StateIdle, pool.StateWaiting, pool.StateExited} {
		first := indicator.Glyph(s, base)
		for i := 1; i < 8; i++ {
			if got := indicator.Glyph(s, base.Add(time.Duration(i)*indicator.Period)); got != first {
				t.Errorf("%v changed from %q to %q", s, first, got)
			}
		}
		if indicator.Animated(s) {
			t.Errorf("%v is reported as animated", s)
		}
	}

	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		seen[indicator.Glyph(pool.StateWorking, base.Add(time.Duration(i)*indicator.Period))] = true
	}
	if len(seen) < 3 {
		t.Errorf("working showed only %d shapes: %v", len(seen), seen)
	}
	if !indicator.Animated(pool.StateWorking) {
		t.Error("working is not reported as animated")
	}
}

// Every state has to look different from every other, or the indicator says
// nothing.
func TestEachStateLooksDifferent(t *testing.T) {
	at := time.Unix(0, 0)
	seen := map[string]pool.State{}
	for _, s := range []pool.State{pool.StateIdle, pool.StateWaiting, pool.StateExited} {
		g := indicator.Glyph(s, at)
		if other, clash := seen[g]; clash {
			t.Errorf("%v and %v share %q", s, other, g)
		}
		seen[g] = s
	}
	colours := map[any]pool.State{}
	for _, s := range []pool.State{pool.StateIdle, pool.StateWorking, pool.StateWaiting, pool.StateExited} {
		c := indicator.Colour(s)
		if other, clash := colours[c]; clash {
			t.Errorf("%v and %v share a colour", s, other)
		}
		colours[c] = s
	}
}

// A set of sessions is summarised by the one that most wants you, not by the
// most common one: something that stopped without being told to is the thing
// you most need to know about.
func TestWorstRanksByWhatWantsYou(t *testing.T) {
	for _, c := range []struct {
		in   []pool.State
		want pool.State
	}{
		{nil, pool.StateIdle},
		{[]pool.State{pool.StateIdle, pool.StateIdle}, pool.StateIdle},
		{[]pool.State{pool.StateIdle, pool.StateWorking}, pool.StateWorking},
		{[]pool.State{pool.StateWorking, pool.StateWorking, pool.StateWaiting}, pool.StateWaiting},
		{[]pool.State{pool.StateWaiting, pool.StateExited}, pool.StateExited},
	} {
		if got := indicator.Worst(c.in); got != c.want {
			t.Errorf("Worst(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
