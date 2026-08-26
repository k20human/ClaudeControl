package usage

import (
	"context"
	"sync"
	"time"

	"claudecontrol/internal/bus"
)

// Topic is where readings are published.
const Topic = "account.usage"

// Reading is what the poller publishes: either a snapshot or the reason there
// is none. Both are worth showing — a panel that goes blank on failure is
// indistinguishable from one reporting nothing used.
type Reading struct {
	Snapshot Snapshot
	Err      error
	At       time.Time
}

// Poller refreshes the account's budgets on a cadence.
//
// The cadence is minutes, not seconds. These are rate-limit budgets over five
// hours and seven days: they move slowly, and asking often would spend the
// account's request allowance to watch it.
type Poller struct {
	client   *Client
	bus      *bus.Bus
	interval time.Duration
	wake     func()

	mu   sync.RWMutex
	last Reading

	done chan struct{}
	once sync.Once
}

// DefaultInterval is how often the budgets are re-read.
const DefaultInterval = 3 * time.Minute

// NewPoller builds a poller. A nil client means the default one.
func NewPoller(c *Client, b *bus.Bus, interval time.Duration, wake func()) *Poller {
	if c == nil {
		c = &Client{}
	}
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Poller{
		client:   c,
		bus:      b,
		interval: interval,
		wake:     wake,
		done:     make(chan struct{}),
	}
}

// Last is the most recent reading, and whether one has been taken at all.
func (p *Poller) Last() (Reading, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.last, !p.last.At.IsZero()
}

// Start begins polling.
func (p *Poller) Start() { go p.loop() }

// Stop ends it. It is safe to call more than once.
func (p *Poller) Stop() { p.once.Do(func() { close(p.done) }) }

// Refresh takes one reading now, out of turn.
func (p *Poller) Refresh() { p.round() }

func (p *Poller) loop() {
	p.round()
	tick := time.NewTicker(p.interval)
	defer tick.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-tick.C:
			p.round()
		}
	}
}

func (p *Poller) round() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	snap, err := p.client.Fetch(ctx)
	r := Reading{Snapshot: snap, Err: err, At: time.Now()}

	p.mu.Lock()
	p.last = r
	p.mu.Unlock()

	if p.bus != nil {
		p.bus.PublishState(Topic, r)
	}
	if p.wake != nil {
		p.wake()
	}
}
