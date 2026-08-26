package hologram

import (
	"image/color"

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
type avatarRenderer struct {
	sig        Signal
	cols, rows int
}

func newAvatar() Renderer { return &avatarRenderer{} }

func (a *avatarRenderer) Resize(cols, rows int) { a.cols, a.rows = cols, rows }
func (a *avatarRenderer) Step(float64)          {}
func (a *avatarRenderer) SetSignal(s Signal)    { a.sig = s }

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
	top := area.Min.Y + (a.rows-len(art))/2
	for i, line := range art {
		y := top + i
		if y < area.Min.Y || y >= area.Max.Y {
			continue
		}
		x := area.Min.X + (a.cols-len([]rune(line)))/2
		render.Text(scr, x, y, line, fg, bgPanel)
	}
}
