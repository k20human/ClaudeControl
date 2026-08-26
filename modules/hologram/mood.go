package hologram

import (
	"math"

	"claudecontrol/internal/holo"
)

// holoParams is an alias kept local so the mood table reads without the
// package name on every line.
type holoParams = holo.Params

// mood is how the sphere moves for one state of the sessions.
//
// The four fields that scale are multipliers over whatever the configuration
// asked for, never absolute values: someone who has set the speed slider to
// their taste should keep their taste in every state. Scatter and pulse have
// no configured base, so they are given outright.
type mood struct {
	speed, rotation, breath float64
	scatter, pulse          float64
}

// moods is the whole of the panel's vocabulary. Density is deliberately absent:
// changing it rebuilds the particles, which reads as a glitch at the exact
// moment a transition should be smooth.
var moods = map[string]mood{
	// At rest. Slow drift, deep breathing, a trace of wander.
	"idle": {speed: 0.55, rotation: 0.70, breath: 1.50, scatter: 0.12},

	// Thinking. Quick and tight: the filaments hold together and the breathing
	// shortens. This is the only state with no scatter at all — a mind
	// following one thread.
	"working": {speed: 2.00, rotation: 1.70, breath: 0.45},

	// Waiting on you. Almost still, swept end to end by a slow band. Nothing
	// is carried anywhere, which is the point: the work is yours now.
	"waiting": {speed: 0.30, rotation: 0.10, breath: 1.20, scatter: 0.05, pulse: 1},

	// Gone. The structure loses its coherence without the sphere leaving.
	"exited": {speed: 0.75, rotation: 0.25, breath: 1.00, scatter: 0.95},
}

// moodFor is the mood for a state, falling back on rest for a state this table
// has never heard of.
func moodFor(state string) mood {
	if m, ok := moods[state]; ok {
		return m
	}
	return moods["idle"]
}

// moodTau is how long a transition takes to all but complete. A second is
// slow enough to read as the panel changing its mind rather than being
// switched, and quick enough that you see it happen.
const moodTau = 1.0

// ease moves a mood towards a target by dt seconds, exponentially: fast at
// first, then settling. Linear interpolation would arrive with a visible stop.
func (m mood) ease(to mood, dt float64) mood {
	if dt <= 0 {
		return m
	}
	k := 1 - math.Exp(-dt/moodTau)
	lerp := func(a, b float64) float64 { return a + (b-a)*k }
	return mood{
		speed:    lerp(m.speed, to.speed),
		rotation: lerp(m.rotation, to.rotation),
		breath:   lerp(m.breath, to.breath),
		scatter:  lerp(m.scatter, to.scatter),
		pulse:    lerp(m.pulse, to.pulse),
	}
}

// apply writes a mood over the configured parameters.
func (m mood) apply(base holoParams) holoParams {
	base.Speed *= m.speed
	base.Rotation *= m.rotation
	if base.Breath > 0 {
		base.Breath *= m.breath
	}
	base.Scatter = m.scatter
	base.Pulse = m.pulse
	return base
}
