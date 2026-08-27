package hologram

import (
	"image/color"
	"math"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/render"
)

// faces are the three expressions, matching the states the user's Divoom
// display already shows. One bit of information, warmly delivered.
var faces = map[string][]string{
	"idle": {
		"╭──────╮",
		"│ ─  ─ │",
		"│      │",
		"│  ⌣⌣  │",
		"╰──────╯",
	},
	"working": {
		"╭──────╮",
		"│ ◉  ◉ │",
		"│  ╱╲  │",
		"│ ──── │",
		"╰──────╯",
	},
	"waiting": {
		"╭──────╮",
		"│ ●  ● │",
		"│  !!  │",
		"│ ⌒⌒⌒⌒ │",
		"╰──────╯",
	},
}

var faceColours = map[string]color.RGBA{
	"idle":    {R: 0x7f, G: 0x8c, B: 0xa0, A: 0xff},
	"working": {R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff},
	"waiting": {R: 0xe5, G: 0x93, B: 0x3a, A: 0xff},
	"exited":  {R: 0xe5, G: 0x6b, B: 0x6b, A: 0xff},
}

// avatarRenderer draws a face for the worst state in the pool.
//
// It moves on the same mood table as the sphere and the ring, but a face
// cannot rotate or scatter without becoming a different face — so only breath
// and pulse are read, as the brightness of the whole and a blink whose rhythm
// belongs to the state. Speed sets the tempo of both.
type avatarRenderer struct {
	sig        Signal
	t          float64
	cols, rows int

	cur, want mood
}

func newAvatar() Renderer {
	start := moodFor("idle")
	return &avatarRenderer{cur: start, want: start}
}

func (a *avatarRenderer) Resize(cols, rows int) { a.cols, a.rows = cols, rows }

func (a *avatarRenderer) Step(dt float64) {
	a.cur = a.cur.ease(a.want, dt)
	a.t += dt * a.cur.speed
}

func (a *avatarRenderer) SetSignal(s Signal) {
	a.sig = s
	a.want = moodFor(s.Worst)
}

// blinkClosed reports whether the eyes are shut at this instant.
//
// Once every blinkEvery seconds, for a tenth of a second. A face that never
// blinks is the one thing that reads as dead, which is the opposite of what
// this panel is for.
const (
	blinkEvery  = 4.7
	blinkLength = 0.12
)

func (a *avatarRenderer) blinkClosed() bool {
	return math.Mod(a.t, blinkEvery) < blinkLength
}

func (a *avatarRenderer) Draw(scr uv.Screen, area uv.Rectangle) {
	render.Fill(scr, area, bgPanel)
	state := a.sig.Worst
	if state == "" {
		state = "idle"
	}
	art, ok := faces[state]
	if !ok {
		// No face is drawn for a state that has none — exited among them,
		// which the colour still reports.
		art = faces["idle"]
	}
	fg, ok := faceColours[state]
	if !ok {
		fg = faceColours["idle"]
	}
	// Breath is the depth of the glow, pulse the state that asks to be
	// noticed: waiting brightens all the way down and back, at rest it barely
	// moves. Both are read through the eased mood, so a change of state fades
	// in rather than switching.
	depth := 0.20*a.cur.breath + 0.35*a.cur.pulse
	if depth > 0.75 {
		depth = 0.75
	}
	glow := (1 - depth) + depth*(0.5+0.5*math.Sin(a.t*1.4))
	lit := mix(bgPanel, fg, glow)

	top := area.Min.Y + (a.rows-len(art))/2
	closed := a.blinkClosed()
	for i, line := range art {
		y := top + i
		if y < area.Min.Y || y >= area.Max.Y {
			continue
		}
		if closed && i == eyeRow {
			line = shutEyes(line)
		}
		x := area.Min.X + (a.cols-len([]rune(line)))/2
		render.Text(scr, x, y, line, lit, bgPanel)
	}
}

// eyeRow is the row of a face the eyes are on. Every face in the table has the
// same shape, which is what lets one rule close all of them.
const eyeRow = 1

// shutEyes replaces whatever the eyes are with a closed lid, leaving the frame
// around them alone.
func shutEyes(line string) string {
	out := []rune(line)
	for i, r := range out {
		switch r {
		case '─', '◉', '●':
			out[i] = '‾'
		}
	}
	return string(out)
}
