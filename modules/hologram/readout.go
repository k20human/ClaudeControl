package hologram

import (
	"fmt"
	"image/color"
	"math/rand"
	"strings"
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
	fgMaxim   = color.RGBA{R: 0x39, G: 0x44, B: 0x57, A: 0xff}
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

// maxims are the second line, in the bottom corner.
//
// They are not the ambient line and are not chosen the same way. The ambient
// line is bound to the state the sessions are in, and so could in principle be
// wrong about it. These assert nothing at all — no activity, no figure, no
// state — which is what makes them safe to show whatever is happening. A
// disposition cannot be false the way a claim can.
var maxims = []string{
	"patterns before conclusions",
	"the shape of it first",
	"slower is often shorter",
	"what is not said, also",
	"every assumption has a cost",
	"the simplest thing that is true",
	"doubt is information",
	"read it twice",
	"the exception is the design",
	"a measure beats an opinion",
}

// maximHold is deliberately much longer than flavourHold. Two lines that
// changed together would read as one animation rather than two thoughts.
const maximHold = 47 * time.Second

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

	maxim      string
	maximSince time.Time

	rng *rand.Rand
}

func newReadout(seed int64) *readout {
	r := &readout{rng: rand.New(rand.NewSource(seed))}
	r.setState("idle", time.Time{})
	r.setMaxim(time.Time{})
	return r
}

// setMaxim draws a fresh line for the corner.
func (r *readout) setMaxim(now time.Time) {
	r.maxim = maxims[r.rng.Intn(len(maxims))]
	r.maximSince = now
}

// Maxim is the corner line.
func (r *readout) Maxim() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxim
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
	// On its own clock, and never on a change of state: the corner line has
	// nothing to do with what the sessions are doing.
	//
	// The first call starts that clock rather than expiring it. Built before
	// there is any time to record, the line would otherwise be replaced the
	// moment the pane is first drawn.
	if r.maximSince.IsZero() {
		r.maximSince = now
	} else if now.Sub(r.maximSince) >= maximHold {
		r.setMaxim(now)
	}
}

// drawMaxim puts the corner line at the bottom right of the pane, dim enough
// to be read only when you look for it.
//
// It is dropped entirely rather than shortened: an aphorism cut in half is not
// a shorter aphorism.
func (r *readout) drawMaxim(scr uv.Screen, area uv.Rectangle) {
	line := r.Maxim()
	w := ansi.StringWidth(line)
	if area.Dy() < 3 || w+2 > area.Dx() {
		return
	}
	render.Text(scr, area.Max.X-1-w, area.Max.Y-1, line, fgMaxim, bgPanel)
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
	// Overlaid, the text takes nothing out of the pane: it is drawn on top of
	// what is already there, which is the whole point of it.
	if side == "" || side == "off" || side == "overlay" || paneW < readoutMinPane {
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

// drawOver paints the text on top of whatever is already in the pane, in the
// top right corner, taking only the rows it has lines for.
//
// A reserved column costs the sphere its width whether or not there is
// anything to put in it, and there usually is not: two lines under a column
// thirty rows tall. Overlaid, each line carries its own background so it stays
// readable against the particles moving behind it — the corner maxim has been
// drawn this way from the start.
func (r *readout) drawOver(scr uv.Screen, area uv.Rectangle, now time.Time) {
	if area.Dx() < readoutMinPane || area.Dy() < 3 {
		return
	}
	width := readoutW
	if half := area.Dx() / 2; width > half {
		width = half
	}
	for i, line := range r.Lines(now) {
		y := area.Min.Y + i
		if y >= area.Max.Y-1 {
			return
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		line = clipTo(line, width-2)
		w := ansi.StringWidth(line)
		x := area.Max.X - 1 - w
		// The background travels with the line and no further: everything
		// around it is sphere, and blanking a rectangle would be the reserved
		// column again.
		render.Fill(scr, uv.Rect(x-1, y, w+2, 1), bgPanel)

		switch {
		case i == 0:
			render.Text(scr, x, y, line, fgFlavour, bgPanel)
		case len(line) > 5 && line[2] == ':':
			// The timestamp is dimmed so the eye lands on what happened, as
			// it is in the column.
			render.Text(scr, x, y, line[:5], fgStamp, bgPanel)
			render.Text(scr, x+5, y, line[5:], fgEvent, bgPanel)
		default:
			render.Text(scr, x, y, line, fgEvent, bgPanel)
		}
	}
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
