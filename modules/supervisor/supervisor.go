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
	"claudecontrol/internal/procs"
	"claudecontrol/internal/relay"
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
	spec Spec
	sess *session.Session

	// adopted is a process this application did not start. It can be watched
	// and stopped, but not read: its output went wherever it was going before
	// we noticed it, and no amount of wanting it back will produce it.
	adopted *procs.Proc

	state State
	code  int
	since time.Time

	// picked is whether the buttons act on it. Everything is picked to begin
	// with: a supervisor whose start button starts nothing would be a puzzle.
	picked bool

	// view is how the log is looked at: the wheel reaches the history, which
	// matters most for the four hundred lines a restart brings back.
	view session.View

	// relay is the directory of the process standing behind a service kept
	// across a restart, and tailStop ends the goroutine pouring its log into
	// the emulator. Empty for a service this application holds directly.
	relay    string
	tailStop chan struct{}

	// relayPid is the service the relay is holding, and relayRead is when its
	// state was last looked at. A pane redraws many times a second and the
	// state is a file: reading it every frame would be a file read a frame.
	relayPid  int
	relayRead time.Time

	// tree is what this service had started, last time anything looked, and
	// left is how many of those were still alive after it ended.
	//
	// A launcher — npm run dev, and most of what a stack is made of — is not
	// the server. Kill the launcher and the server keeps its port, which is
	// the state that makes a row reading "exited" so misleading: true of the
	// process, and false of the service. Sampled while it runs because by the
	// time it has ended its children have been re-parented and there is
	// nothing left to walk down to.
	tree []int
	left int

	// carry is what the previous run printed, put back on the screen when the
	// next one starts. A restart usually happens because of something the
	// service said, and losing that at the moment you act on it is the worst
	// possible time to lose it.
	carry string
}

// Module is the pane.
type Module struct {
	ctx   module.Context
	grace time.Duration

	// keep says whether services outlive the application. When they do they
	// run under a relay, which is what keeps them off a terminal nobody is
	// draining once it has gone.
	keep bool

	// binary is this application, which is also the relay.
	binary string

	mu   sync.Mutex
	svcs []*service

	// sel is the highlighted row, and showing is the service whose output
	// fills the pane, or -1 for the list.
	sel     int
	showing int

	cols, rows int

	// scanning stops when the module closes.
	scanning chan struct{}
	scanOnce sync.Once
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
	m := &Module{grace: DefaultGrace, showing: -1, scanning: make(chan struct{})}
	if v, ok := toFloat(cfg["stop_grace"]); ok && v > 0 {
		m.grace = time.Duration(v * float64(time.Second))
	}
	if v, ok := cfg["keep_running"].(bool); ok {
		m.keep = v
	}
	// The relay is this application in another mode, so it has to know where
	// it lives. A failure here only costs the keeping, not the supervising.
	if bin, err := os.Executable(); err == nil {
		m.binary = bin
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
	// Before anything starts: what the last run of the application left is
	// what a service that starts now should open with.
	m.loadCarry()
	m.mu.Lock()
	for _, s := range m.svcs {
		// A service kept across a restart is found before anything is
		// started: it is already running, and starting a second copy is how
		// two servers come to fight over one port.
		if m.keep && m.attachKeptLocked(s) {
			continue
		}
		if s.spec.Autostart {
			m.startLocked(s)
		}
	}
	m.mu.Unlock()

	// A service you started in another terminal is still your service. Finding
	// it now means the pane opens showing what is true rather than claiming
	// everything is down.
	m.Scan()
	go m.scanLoop()
	return nil
}

// ScanInterval is how often already-running services are looked for. Slow on
// purpose: it walks /proc, and a service you started elsewhere is not urgent
// news.
const ScanInterval = 5 * time.Second

func (m *Module) scanLoop() {
	tick := time.NewTicker(ScanInterval)
	defer tick.Stop()
	for {
		select {
		case <-m.scanning:
			return
		case <-tick.C:
			m.Scan()
		}
	}
}

// Scan looks for services already running and takes them over.
//
// Only for services this module is not already running: one it started is
// known exactly, and one it has adopted is checked by Alive instead.
func (m *Module) Scan() {
	m.mu.Lock()
	wanted := false
	for _, s := range m.svcs {
		if m.adoptableLocked(s) || (s.sess != nil && s.state == Running) {
			wanted = true
			break
		}
	}
	m.mu.Unlock()
	if !wanted {
		return
	}

	// One walk of /proc for every service, rather than one per service.
	all, err := procs.All()
	if err != nil {
		return
	}

	children := map[int][]int{}
	for _, p := range all {
		children[p.PPID] = append(children[p.PPID], p.Pid)
	}

	m.mu.Lock()
	changed := false
	for _, s := range m.svcs {
		// What it has started, while it is still there to be asked.
		if s.sess != nil && s.state == Running {
			if pid := s.sess.Pid(); pid > 0 {
				s.tree = descend(children, pid)
			}
		}
		if !m.adoptableLocked(s) {
			continue
		}
		for i := range all {
			if !matches(all[i], s.spec) {
				continue
			}
			found := all[i]
			// A session that has ended is dropped, its output kept: the row is
			// about to describe a process this application did not start, and
			// two accounts of one service on one row is one too many.
			if s.sess != nil {
				m.captureLocked(s)
				_ = s.sess.Close()
				s.sess = nil
			}
			s.adopted, s.state, s.since, s.left = &found, Running, time.Now(), 0
			changed = true
			break
		}
	}
	m.mu.Unlock()

	if changed && m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

// adoptableLocked reports whether a service could take over a process found
// running.
//
// Not while it holds one of its own, and not while it holds a live session.
// But a session that has ended is no bar: the service is not running, whatever
// the row still says, and a matching process out there is the truth. Without
// this an ended service could never be noticed alive again, and the scan
// button had nothing to correct.
func (m *Module) adoptableLocked(s *service) bool {
	if s.adopted != nil || s.spec.Dir == "" {
		return false
	}
	return s.sess == nil || s.state == Exited
}

// descend collects a process and everything under it, from an index built once
// for the whole walk.
func descend(children map[int][]int, pid int) []int {
	out := []int{pid}
	for i := 0; i < len(out); i++ {
		out = append(out, children[out[i]]...)
	}
	return out[1:]
}

// matches reports whether a process is this service, by what it runs and where.
// The directory alone would not do: a project can run two scripts at once,
// which is exactly what a front end and its back end do.
func matches(p procs.Proc, spec Spec) bool {
	// Through procs.SameArgv rather than compared here, because there is one
	// rule about what "the same command" means and it belongs in one place. A
	// second copy of it is a second thing to be wrong: this one compared
	// argument by argument, and so could never recognise a service whose
	// program had renamed itself — which npm does to every one of them.
	return p.Cwd == spec.Dir && procs.SameArgv(p.Cmdline, spec.Argv)
}

// tellRelaysLocked passes the pane's size on to the terminals the relays own,
// so a service's output is wrapped for the width it will be read at.
func (m *Module) tellRelaysLocked(w, h int) {
	for _, s := range m.svcs {
		if s.relay != "" {
			_ = relay.SetSize(s.relay, w, h)
		}
	}
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
	if lw > 0 && lh > 0 {
		m.tellRelaysLocked(lw, lh)
	}
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
	m.scanOnce.Do(func() { close(m.scanning) })
	// Written before the processes are stopped: what is on the screen now is
	// the record, and terminating first would race the last lines away.
	m.saveCarry()
	m.mu.Lock()
	svcs := append([]*service(nil), m.svcs...)
	grace := m.grace
	m.mu.Unlock()

	// Concurrently: each one is given its full grace period, and doing that in
	// turn would make quitting take grace times the number of services.
	var wg sync.WaitGroup
	for _, s := range svcs {
		// A kept service is left exactly as it is: its relay owns the terminal
		// and goes on draining it, so nothing here has to end for it to keep
		// running. Only the pouring of its log into this pane stops.
		if s.relay != "" {
			if s.tailStop != nil {
				close(s.tailStop)
				s.tailStop = nil
			}
			continue
		}
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
		if s.state != Running {
			continue
		}
		if s.adopted != nil {
			// No exit code: we never waited on it, so we do not know how it
			// ended and will not invent a number.
			if !s.adopted.Alive() {
				s.adopted, s.state, s.since = nil, Stopped, time.Now()
			}
			continue
		}
		if s.relay != "" {
			m.refreshKeptLocked(s)
			continue
		}
		if s.sess == nil {
			continue
		}
		if st, code := s.sess.Status(); st == session.Exited {
			s.state, s.code, s.since = Exited, code, time.Now()
			s.left = stillAlive(s.tree)
		}
	}
}

// stillAlive counts how many of these processes are still there.
func stillAlive(pids []int) int {
	n := 0
	for _, pid := range pids {
		if p, err := procs.Read(pid); err == nil && p.Alive() {
			n++
		}
	}
	return n
}

// startLocked launches a service that is not already up.
func (m *Module) startLocked(s *service) {
	if s.state == Running {
		return
	}
	if s.adopted != nil && s.adopted.Alive() {
		// Already up, elsewhere. Starting a second copy is how two servers
		// come to fight over one port.
		s.state = Running
		return
	}
	s.adopted = nil
	if s.sess != nil {
		m.captureLocked(s)
		if s.tailStop != nil {
			close(s.tailStop)
			s.tailStop = nil
		}
		_ = s.sess.Close()
		s.sess = nil
	}
	if m.keep && m.binary != "" {
		m.startKeptLocked(s)
		return
	}
	s.relay = ""
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
	s.tree, s.left = nil, 0
	replay(sess, s.carry, w)
	// The offset belonged to a screen that no longer exists. Keeping it would
	// open the new run somewhere in the middle of the old one.
	s.view.Reset()
	s.sess, s.state, s.code, s.since = sess, Running, 0, time.Now()
}

// stopLocked ends a service, politely first.
func (m *Module) stopLocked(s *service) {
	if s.adopted != nil {
		p, grace := *s.adopted, m.grace
		s.adopted, s.state, s.since = nil, Stopped, time.Now()
		go func() {
			_ = p.Terminate(grace)
			if m.ctx.Wake != nil {
				m.ctx.Wake()
			}
		}()
		return
	}
	if s.relay != "" {
		m.stopKeptLocked(s)
		return
	}
	if s.sess == nil {
		s.state = Stopped
		return
	}
	m.captureLocked(s)
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
		s       *service
		sess    *session.Session
		adopted *procs.Proc
	}
	going := make([]pair, 0, len(picked))
	for _, s := range picked {
		// Before the session is let go: what it printed is the reason this
		// button was pressed.
		m.captureLocked(s)
		going = append(going, pair{s, s.sess, s.adopted})
		s.sess, s.adopted, s.state = nil, nil, Stopped
	}
	m.mu.Unlock()

	go func() {
		var wg sync.WaitGroup
		for _, p := range going {
			switch {
			case p.sess != nil:
				wg.Add(1)
				go func(sess *session.Session) {
					defer wg.Done()
					_ = sess.Terminate(grace)
				}(p.sess)
			case p.adopted != nil:
				// Restarting is how an adopted service comes back under
				// supervision, and with it the output that could not be read
				// before.
				wg.Add(1)
				go func(proc procs.Proc) {
					defer wg.Done()
					_ = proc.Terminate(grace)
				}(*p.adopted)
			}
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

	// Adopted marks a process this application did not start. It can be
	// watched and stopped; its output cannot be read.
	Adopted bool

	// Children is how many processes this service had started, last time
	// anything looked. Left is how many of those outlived it.
	Children int
	Left     int
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
			Children: len(s.tree), Left: s.left,
		}
		switch {
		case s.relay != "":
			// An attached session has no process of its own: the one that
			// matters is the service the relay is holding.
			snap.Pid = s.relayPid
		case s.sess != nil:
			snap.Pid = s.sess.Pid()
		case s.adopted != nil:
			snap.Pid, snap.Adopted = s.adopted.Pid, true
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
