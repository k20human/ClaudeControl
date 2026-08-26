package hologram

import (
	"fmt"
	"image/color"
	"math/rand"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/render"
	"claudecontrol/internal/transcript"
)

var (
	fgFlavour = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	fgStamp   = color.RGBA{R: 0x3d, G: 0x47, B: 0x58, A: 0xff}
	fgEvent   = color.RGBA{R: 0x8a, G: 0x98, B: 0xad, A: 0xff}
)

const (
	// readoutMinPane is the pane width below which the sphere keeps
	// everything. A column narrower than this holds nothing legible, and half
	// a sentence is worse than none.
	readoutMinPane = 44
	// readoutW is the column's width when there is room for it.
	readoutW = 28
	// logDepth is how many events are kept. Enough to see the shape of the
	// last few minutes, few enough to fit a short pane.
	logDepth = 8
)

// flavours are the ambient line, one pool per state.
//
// They are decoration, and decoration on a control panel has exactly one rule:
// it must never be mistakable for a statement about what the machine is doing.
// So each line is drawn from the pool of the state the sessions are actually
// in, and none of them cites a number or names an operation. A phrase claiming
// "cross-referencing four sources" while nothing runs is a lie on an
// instrument panel, and it costs you the right to believe the rest of it.
var flavours = map[string][]string{
	"idle": {
		"standing by",
		"listening",
		"nothing pending",
		"at rest",
	},
	"working": {
		"following the thread",
		"weighing several paths",
		"turning it over",
		"narrowing the field",
		"holding the shape of it",
	},
	"waiting": {
		"your move",
		"awaiting your word",
		"the floor is yours",
	},
	"exited": {
		"session closed",
		"gone quiet",
	},
}

// flavourHold is how long a line stays before another is drawn from the same
// pool. A phrase that changed every second would read as noise.
const flavourHold = 20 * time.Second

// event is one line of the log.
type event struct {
	at   time.Time
	text string
}

// readout is the column of text beside the sphere: an ambient line, then what
// has actually happened.
//
// Everything below the ambient line is a real event — a session appearing, a
// state changing, a turn landing. No conversation content ever reaches it: a
// decorative panel that echoes what Claude writes puts your work on any screen
// the terminal is shown on.
type readout struct {
	mu      sync.Mutex
	log     []event
	state   string
	flavour string
	since   time.Time
	rng     *rand.Rand
}

func newReadout(seed int64) *readout {
	r := &readout{rng: rand.New(rand.NewSource(seed))}
	r.setState("idle", time.Time{})
	return r
}

// setState records the state and draws a fresh ambient line for it.
func (r *readout) setState(state string, now time.Time) {
	r.state = state
	pool := flavours[state]
	if len(pool) == 0 {
		pool = flavours["idle"]
	}
	r.flavour = pool[r.rng.Intn(len(pool))]
	r.since = now
}

// Note adds an event, dropping the oldest once the log is full.
func (r *readout) Note(at time.Time, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, event{at: at, text: text})
	if len(r.log) > logDepth {
		r.log = r.log[len(r.log)-logDepth:]
	}
}

// Observe follows the state, changing the ambient line when the state changes
// and once in a while otherwise.
func (r *readout) Observe(state string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state != r.state || now.Sub(r.since) >= flavourHold {
		r.setState(state, now)
	}
}

// Lines is the column's contents, newest event first.
func (r *readout) Lines(now time.Time) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.log)+2)
	out = append(out, "▸ "+r.flavour, "")
	for i := len(r.log) - 1; i >= 0; i-- {
		e := r.log[i]
		out = append(out, e.at.Format("15:04")+"  "+e.text)
	}
	return out
}

// turnLine describes a turn without quoting it.
func turnLine(met transcript.Metrics) string {
	if met.Context <= 0 {
		return "turn"
	}
	return fmt.Sprintf("turn ▸ %s ctx", transcript.HumanTokens(met.Context))
}

// columnW is how many columns the text takes out of a pane of this width, and
// zero when there is no room for it. Sizing and drawing both go through this,
// or the sphere would be built for one width and painted into another.
func columnW(paneW int, side string) int {
	if side == "" || side == "off" || paneW < readoutMinPane {
		return 0
	}
	w := readoutW
	if half := paneW / 2; w > half {
		w = half
	}
	return w
}

// readoutSplit divides a pane between the sphere and the column, and reports
// whether there is room for a column at all.
func readoutSplit(area uv.Rectangle, side string) (sphere, column uv.Rectangle, ok bool) {
	w := columnW(area.Dx(), side)
	if w == 0 {
		return area, uv.Rectangle{}, false
	}
	if side == "left" {
		return uv.Rect(area.Min.X+w, area.Min.Y, area.Dx()-w, area.Dy()),
			uv.Rect(area.Min.X, area.Min.Y, w, area.Dy()), true
	}
	return uv.Rect(area.Min.X, area.Min.Y, area.Dx()-w, area.Dy()),
		uv.Rect(area.Max.X-w, area.Min.Y, w, area.Dy()), true
}

// draw paints the column.
func (r *readout) draw(scr uv.Screen, area uv.Rectangle, now time.Time) {
	render.Fill(scr, area, bgPanel)
	if area.Dx() < 4 {
		return
	}
	x, w := area.Min.X+1, area.Dx()-2
	for i, line := range r.Lines(now) {
		y := area.Min.Y + i
		if y >= area.Max.Y {
			return
		}
		fg := color.Color(fgEvent)
		switch {
		case i == 0:
			fg = fgFlavour
		case len(line) > 5 && line[2] == ':':
			// The timestamp is dimmed so the eye lands on what happened.
			render.Text(scr, x, y, line[:5], fgStamp, bgPanel)
			render.Text(scr, x+5, y, clipTo(line[5:], w-5), fgEvent, bgPanel)
			continue
		}
		render.Text(scr, x, y, clipTo(line, w), fg, bgPanel)
	}
}

func clipTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}
