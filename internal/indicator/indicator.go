// Package indicator says what a session is doing, in one glyph.
//
// The same vocabulary in every place it appears: the title above a pane, the
// strip of a pane of tabs, and the title of the terminal the whole application
// runs in. One meaning per shape, so a glance anywhere reads the same.
//
// The glyph is derived from the session state the hooks report, not from the
// title Claude Code writes for itself. That title says the same thing, but as
// a string this application would have to guess at; the state arrives typed.
package indicator

import (
	"image/color"
	"time"

	"claudecontrol/internal/pool"
)

// working turns. Nothing else does — a shape that moves means work is
// happening, and if everything moved that would mean nothing.
var working = []string{"✳", "✻", "✽", "✻"}

// Period is how long a frame lasts. Slow enough to read as deliberate rather
// than frantic, quick enough to be plainly alive.
const Period = 220 * time.Millisecond

var (
	// fgWorking is the calm colour: work in progress is the normal case and
	// should not shout.
	fgWorking = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	// fgWaiting is the one that wants you. It is the only warm colour here.
	fgWaiting = color.RGBA{R: 0xe0, G: 0xb0, B: 0x5c, A: 0xff}
	fgExited  = color.RGBA{R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff}
	fgIdle    = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
)

// Glyph is the mark for a state at a moment. Only the working one depends on
// the moment.
func Glyph(s pool.State, at time.Time) string {
	switch s {
	case pool.StateWorking:
		i := int(at.UnixNano()/int64(Period)) % len(working)
		if i < 0 {
			i += len(working)
		}
		return working[i]
	case pool.StateWaiting:
		return "◐"
	case pool.StateExited:
		return "✕"
	}
	return "·"
}

// Colour is the state's colour, the same wherever the glyph is drawn.
func Colour(s pool.State) color.Color {
	switch s {
	case pool.StateWorking:
		return fgWorking
	case pool.StateWaiting:
		return fgWaiting
	case pool.StateExited:
		return fgExited
	}
	return fgIdle
}

// Animated reports whether a state's glyph changes over time, which is what
// decides whether the interface has to keep redrawing.
func Animated(s pool.State) bool { return s == pool.StateWorking }

// Worst is the state a set of sessions should be summarised by: the one that
// most wants your attention, not the most common one.
//
// A session that has ended outranks one asking a question, which outranks one
// working. Something that stopped without being told to is the thing you most
// need to know about.
func Worst(states []pool.State) pool.State {
	worst := pool.StateIdle
	for _, s := range states {
		if rank(s) > rank(worst) {
			worst = s
		}
	}
	return worst
}

func rank(s pool.State) int {
	switch s {
	case pool.StateExited:
		return 3
	case pool.StateWaiting:
		return 2
	case pool.StateWorking:
		return 1
	}
	return 0
}
