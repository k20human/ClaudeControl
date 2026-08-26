// Package supervisor runs the long-lived processes a local stack is made of,
// and shows what each of them is doing.
//
// It is not the tasks module. A task runs once and you read what it printed; a
// service is meant to stay up, and what matters is whether it still is. Nor is
// it the services module, which asks whether something already running answers
// — this one owns the processes.
package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"claudecontrol/internal/module"
	"claudecontrol/internal/session"
)

func init() { module.Register("supervisor", New) }

// DefaultGrace is how long a service is given to stop before it is killed.
const DefaultGrace = 5 * time.Second

// Spec is one configured service.
type Spec struct {
	Name      string
	Argv      []string
	Dir       string
	Env       []string
	Autostart bool

	// Optional leaves the service unticked, so the buttons skip it until you
	// say otherwise. It is still listed and still startable on its own — the
	// point is that "start" means the usual set, not everything that exists.
	Optional bool
}

// State is what a service is doing.
type State int

const (
	// Stopped means it has never run, or was stopped on purpose.
	Stopped State = iota
	// Running means the process is alive.
	Running
	// Exited means it ended on its own. The code says how.
	Exited
)

func (s State) String() string {
	switch s {
	case Running:
		return "running"
	case Exited:
		return "exited"
	}
	return "stopped"
}

// service is one configured process and whatever is currently true of it.
type service struct {
	spec  Spec
	sess  *session.Session
	state State
	code  int
	since time.Time

	// picked is whether the buttons act on it. Everything is picked to begin
	// with: a supervisor whose start button starts nothing would be a puzzle.
	picked bool
}

// Module is the pane.
type Module struct {
	ctx   module.Context
	grace time.Duration

	mu   sync.Mutex
	svcs []*service

	// sel is the highlighted row, and showing is the service whose output
	// fills the pane, or -1 for the list.
	sel     int
	showing int

	cols, rows int
	// hits are the clickable regions, republished on every frame rather than
	// held in a table that could drift from what is drawn.
	hits []hit
}

// hit is a region and what clicking it does.
type hit struct {
	x, y, w int
	run     func(m *Module)
}

// New builds the module from its configuration.
func New(cfg map[string]any) (module.Module, error) {
	m := &Module{grace: DefaultGrace, showing: -1}
	if v, ok := toFloat(cfg["stop_grace"]); ok && v > 0 {
		m.grace = time.Duration(v * float64(time.Second))
	}

	raw, _ := cfg["services"].([]any)
	for _, item := range raw {
		spec, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("supervisor: each service must be a mapping, got %T", item)
		}
		s, err := specFrom(spec)
		if err != nil {
			return nil, err
		}
		m.svcs = append(m.svcs, &service{spec: s, picked: !s.Optional})
	}
	if len(m.svcs) == 0 {
		return nil, fmt.Errorf("supervisor: no %q configured", "services")
	}
	return m, nil
}

func specFrom(raw map[string]any) (Spec, error) {
	name, _ := raw["name"].(string)
	if name == "" {
		return Spec{}, fmt.Errorf("supervisor: a service has no %q", "name")
	}
	argv, ok := toStrings(raw["cmd"])
	if !ok || len(argv) == 0 {
		return Spec{}, fmt.Errorf("supervisor: service %q has no %q", name, "cmd")
	}
	env, _ := toStrings(raw["env"])
	dir, _ := raw["dir"].(string)
	auto, _ := raw["autostart"].(bool)
	opt, _ := raw["optional"].(bool)
	if auto && opt {
		return Spec{}, fmt.Errorf(
			"supervisor: service %q is both %q and %q; one says start it now, the other says leave it out",
			name, "autostart", "optional")
	}
	return Spec{
		Name:      name,
		Argv:      argv,
		Dir:       expand(dir),
		Env:       env,
		Autostart: auto,
		Optional:  opt,
	}, nil
}

// expand resolves a leading ~ as well as environment variables, because a
// configuration file is written by hand.
func expand(p string) string {
	if p == "" {
		return ""
	}
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Init starts whatever asked to be started.
//
// Nothing starts by default. A pane that spawned a dozen development servers
// the moment it appeared would be a surprise, and the one thing a supervisor
// must never be is surprising about what is running.
func (m *Module) Init(ctx module.Context) error {
	m.ctx = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.svcs {
		if s.spec.Autostart {
			m.startLocked(s)
		}
	}
	return nil
}

// Title names the pane for what it holds.
func (m *Module) Title() (string, bool) { return "services", true }

// Resize records the size and passes it to every hosted process, so a service
// that has never been looked at still wraps its output correctly.
func (m *Module) Resize(w, h int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cols, m.rows = w, h
	lw, lh := logArea(w, h)
	for _, s := range m.svcs {
		if s.sess != nil && lw > 0 && lh > 0 {
			_ = s.sess.Resize(lw, lh)
		}
	}
	return nil
}

// logArea is the size a hosted process is given: the pane less the header.
func logArea(w, h int) (int, int) {
	return w, h - headerRows
}

// Close ends every service. This is what keeps a quit from leaving a stack
// running with nothing left to manage it.
func (m *Module) Close() error {
	m.mu.Lock()
	svcs := append([]*service(nil), m.svcs...)
	grace := m.grace
	m.mu.Unlock()

	// Concurrently: each one is given its full grace period, and doing that in
	// turn would make quitting take grace times the number of services.
	var wg sync.WaitGroup
	for _, s := range svcs {
		if s.sess == nil {
			continue
		}
		wg.Add(1)
		go func(s *service) {
			defer wg.Done()
			_ = s.sess.Terminate(grace)
		}(s)
	}
	wg.Wait()
	return nil
}

// refreshLocked notices processes that ended on their own.
//
// A service that dies stays dead and says so. Bringing it back would hide the
// failure behind a row that reads "running" while nothing works.
func (m *Module) refreshLocked() {
	for _, s := range m.svcs {
		if s.state != Running || s.sess == nil {
			continue
		}
		if st, code := s.sess.Status(); st == session.Exited {
			s.state, s.code, s.since = Exited, code, time.Now()
		}
	}
}

// startLocked launches a service that is not already up.
func (m *Module) startLocked(s *service) {
	if s.state == Running {
		return
	}
	if s.sess != nil {
		_ = s.sess.Close()
		s.sess = nil
	}
	w, h := logArea(m.cols, m.rows)
	if w < 1 || h < 1 {
		w, h = 80, 24
	}
	sess, err := session.Start(session.Spec{
		ID:       session.ID("supervisor/" + s.spec.Name),
		Argv:     s.spec.Argv,
		Dir:      s.spec.Dir,
		Env:      s.spec.Env,
		Width:    w,
		Height:   h,
		OnUpdate: m.ctx.Wake,
	})
	if err != nil {
		s.state, s.code, s.since = Exited, -1, time.Now()
		return
	}
	s.sess, s.state, s.code, s.since = sess, Running, 0, time.Now()
}

// stopLocked ends a service, politely first.
func (m *Module) stopLocked(s *service) {
	if s.sess == nil {
		s.state = Stopped
		return
	}
	sess, grace := s.sess, m.grace
	s.sess, s.state, s.since = nil, Stopped, time.Now()
	// Off the draw path: a service that ignores SIGTERM would otherwise freeze
	// the interface for the whole grace period.
	go func() {
		_ = sess.Terminate(grace)
		if m.ctx.Wake != nil {
			m.ctx.Wake()
		}
	}()
}

// picked reports the services the buttons act on.
func (m *Module) pickedLocked() []*service {
	out := make([]*service, 0, len(m.svcs))
	for _, s := range m.svcs {
		if s.picked {
			out = append(out, s)
		}
	}
	return out
}

// StartPicked, StopPicked and RestartPicked are the three buttons.
func (m *Module) StartPicked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshLocked()
	for _, s := range m.pickedLocked() {
		m.startLocked(s)
	}
}

func (m *Module) StopPicked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.pickedLocked() {
		m.stopLocked(s)
	}
}

// RestartPicked stops and starts. The stop is asynchronous, so the start waits
// for it: launching a server before the old one has let go of its port is the
// one thing a restart button must not do.
func (m *Module) RestartPicked() {
	m.mu.Lock()
	picked := m.pickedLocked()
	grace := m.grace
	type pair struct {
		s    *service
		sess *session.Session
	}
	going := make([]pair, 0, len(picked))
	for _, s := range picked {
		going = append(going, pair{s, s.sess})
		s.sess, s.state = nil, Stopped
	}
	m.mu.Unlock()

	go func() {
		var wg sync.WaitGroup
		for _, p := range going {
			if p.sess == nil {
				continue
			}
			wg.Add(1)
			go func(sess *session.Session) {
				defer wg.Done()
				_ = sess.Terminate(grace)
			}(p.sess)
		}
		wg.Wait()

		m.mu.Lock()
		for _, p := range going {
			m.startLocked(p.s)
		}
		m.mu.Unlock()
		if m.ctx.Wake != nil {
			m.ctx.Wake()
		}
	}()
}

// Snapshot is what the pane shows, for tests and for anything that wants to
// know without drawing.
type Snapshot struct {
	Name   string
	State  State
	Code   int
	Pid    int
	Since  time.Time
	Picked bool
}

// Services is the current state of every configured service.
func (m *Module) Services() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshLocked()
	out := make([]Snapshot, 0, len(m.svcs))
	for _, s := range m.svcs {
		snap := Snapshot{
			Name: s.spec.Name, State: s.state, Code: s.code,
			Since: s.since, Picked: s.picked,
		}
		if s.sess != nil {
			snap.Pid = s.sess.Pid()
		}
		out = append(out, snap)
	}
	return out
}

func toFloat(v any) (float64, bool) {
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

// toStrings reads a command line from the configuration.
//
// Scalars are converted rather than refused: YAML reads a bare true as a
// boolean and a bare 3000 as a number, so "cmd: [true]" would otherwise be an
// error about a type the author never typed.
func toStrings(v any) ([]string, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch s := item.(type) {
		case string:
			out = append(out, s)
		case bool, int, int64, float64:
			out = append(out, fmt.Sprintf("%v", s))
		default:
			return nil, false
		}
	}
	return out, true
}
