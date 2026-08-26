package probe

import (
	"context"
	"sync"
	"time"

	"claudecontrol/internal/bus"
)

// HealthTopic is where results are published.
const HealthTopic = "service.health"

// Runner asks its probes on a cadence.
type Runner struct {
	bus      *bus.Bus
	interval time.Duration
	timeout  time.Duration

	mu      sync.RWMutex
	probes  []Probe
	results []Result

	done chan struct{}
	once sync.Once
}

// NewRunner builds a runner. The timeout bounds each individual probe.
func NewRunner(b *bus.Bus, interval, timeout time.Duration) *Runner {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Runner{
		bus:      b,
		interval: interval,
		timeout:  timeout,
		done:     make(chan struct{}),
	}
}

// Add registers a probe. Its first result is unknown until it has run.
func (r *Runner) Add(p Probe) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probes = append(r.probes, p)
	r.results = append(r.results, Result{Name: p.Name, Health: HealthUnknown})
}

// Results are the latest answers, in the order the probes were added.
func (r *Runner) Results() []Result {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Result(nil), r.results...)
}

// Start begins asking.
func (r *Runner) Start() { go r.loop() }

// Stop ends it. It is safe to call more than once.
func (r *Runner) Stop() { r.once.Do(func() { close(r.done) }) }

func (r *Runner) loop() {
	r.round()
	tick := time.NewTicker(r.interval)
	defer tick.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-tick.C:
			r.round()
		}
	}
}

// round asks every probe at once.
//
// Concurrently, and each with its own deadline: one endpoint that hangs must
// not decide how long the whole round takes, and a display waiting on the
// round would otherwise freeze with it.
func (r *Runner) round() {
	r.mu.RLock()
	probes := append([]Probe(nil), r.probes...)
	r.mu.RUnlock()

	var wg sync.WaitGroup
	found := make([]Result, len(probes))
	for i, p := range probes {
		wg.Add(1)
		go func(i int, p Probe) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
			defer cancel()
			start := time.Now()
			health, detail := p.Check(ctx)
			found[i] = Result{
				Name:   p.Name,
				Health: health,
				Detail: detail,
				Took:   time.Since(start),
			}
		}(i, p)
	}
	wg.Wait()

	r.mu.Lock()
	r.results = found
	r.mu.Unlock()

	if r.bus != nil {
		r.bus.PublishState(HealthTopic, found)
	}
}
