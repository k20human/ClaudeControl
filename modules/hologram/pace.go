package hologram

import (
	"sync"
	"time"
)

// How often the panel asks to be redrawn.
//
// The application redraws when something asks it to, and this module used to
// ask at the end of every frame it drew — which, against a sixty-a-second
// timer, meant sixty full-screen repaints a second for as long as the panel
// was on screen. Measured: a sphere in a pane of 120 by 30 cost about fifteen
// per cent of a core, of which only five was the sphere. The other ten was
// redrawing, diffing and writing the whole screen, over and over, for an
// animation nobody can follow at that rate.
//
// So the panel keeps its own pace. It still draws whenever the application
// draws — it must, it is on screen — but it only *asks* for the next frame at
// the rate configured, and asking is what sets the rate.

// DefaultFPS is the pace a drifting sphere is drawn at. Fast enough that the
// motion is smooth to the eye and slow enough to cost a third of what sixty
// did; anything above this is spending a core on frames you cannot see.
const DefaultFPS = 20

// maxFPS is the application's own ceiling. Asking for more than the draw loop
// can give is asking for nothing.
const maxFPS = 60

// pacer asks for the next frame, once, at the right time.
type pacer struct {
	mu       sync.Mutex
	interval time.Duration
	pending  bool
	timer    *time.Timer
	stopped  bool
}

func newPacer(fps float64) *pacer {
	if fps <= 0 {
		fps = DefaultFPS
	}
	if fps > maxFPS {
		fps = maxFPS
	}
	return &pacer{interval: time.Duration(float64(time.Second) / fps)}
}

// ask schedules one wake, no sooner than the interval and no more than one at
// a time: a frame that arrives while one is already pending would double the
// rate rather than keep it.
func (p *pacer) ask(wake func()) {
	if p == nil || wake == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending || p.stopped {
		return
	}
	p.pending = true
	p.timer = time.AfterFunc(p.interval, func() {
		p.mu.Lock()
		p.pending = false
		p.mu.Unlock()
		wake()
	})
}

// stop drops a pending wake, so a closed panel stops asking.
func (p *pacer) stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.pending = false
}

// asFloat reads a number however the configuration spelled it.
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
