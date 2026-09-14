package supervisor

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"claudecontrol/internal/relay"
	"claudecontrol/internal/session"
)

// replayCap is how much of a log is put back on screen when a pane attaches to
// a service that was already running. Enough to fill any pane several times
// over, and far less than the whole file: what matters is what it says now.
const replayCap = 256 << 10

// relayReadEvery is how often a relay's published state is re-read. A pane
// redraws many times a second; a service's lifecycle does not change that fast.
const relayReadEvery = 400 * time.Millisecond

// tailInterval is how often the log is checked for more. Fast enough to read
// as live, slow enough to cost nothing.
const tailInterval = 100 * time.Millisecond

// relayDir is where a service's relay keeps its terminal, its log and what it
// has published about itself.
func (m *Module) relayDir(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return filepath.Join(LogDir(), fmt.Sprintf("%s-%04x", safeName(name), h.Sum32()&0xffff))
}

// startKeptLocked starts a service under a relay, and attaches to its output.
//
// The relay is what makes the service outlive this application: it stands in
// its own session, owns the terminal the service writes to, and empties it
// into a file. The service sees a terminal like any other — colours, progress
// bars — and the file holds those bytes exactly as they arrived, which is what
// lets them come back on screen.
func (m *Module) startKeptLocked(s *service) {
	dir := m.relayDir(s.spec.Name)
	// A relay may already be serving this directory — one from an earlier run
	// of this application, or one whose record a later start overwrote. Taking
	// it over is the whole of what "already running" means here; starting a
	// second beside it is how two servers came to fight over one port.
	if m.attachKeptLocked(s) {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.state, s.code, s.since = Exited, -1, time.Now()
		return
	}
	// The relay reads where to run from here, since its own working directory
	// is wherever the application happened to be.
	writeRelayState(dir, relay.State{Dir: s.spec.Dir})

	w, h := logArea(m.cols, m.rows)
	if w < 1 || h < 1 {
		w, h = 120, 40
	}
	_ = relay.SetSize(dir, w, h)
	// A log from the run before this one is not this run's.
	_ = os.Remove(filepath.Join(dir, relay.LogFile))

	argv := append([]string{"--relay", dir}, s.spec.Argv...)
	cmd := exec.Command(m.binary, argv...)
	cmd.Env = append(os.Environ(), s.spec.Env...)
	// Its own session: a signal sent to this application's group must never
	// reach it, which is the whole of why it survives.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		s.state, s.code, s.since = Exited, -1, time.Now()
		return
	}
	// Reaped here and now: the relay outlives us, so nothing else will.
	go func() { _, _ = cmd.Process.Wait() }()

	s.relay = dir
	s.relayPid, s.relayRead = 0, time.Time{}
	s.state, s.code, s.since = Running, 0, time.Now()
	s.tree, s.left = nil, 0
	m.attachLocked(s, w, h, false)
}

// attachKeptLocked takes over a relay that is already running, which is how a
// service kept across a restart comes back — with its output, unlike one
// adopted from a bare process.
func (m *Module) attachKeptLocked(s *service) bool {
	dir := m.relayDir(s.spec.Name)
	st, ok := relay.ReadState(dir)
	if !ok || st.Exited || st.Pid <= 0 || !aliveNow(st.Pid) {
		// The file says nothing is running here. It can be wrong in the one
		// direction that costs: a start overwrites it, so a relay from an
		// earlier run has no record left. Ask the process table, which no
		// start can rewrite.
		found := liveRelays(dir)
		if len(found) == 0 {
			return false
		}
		st.Relay, st.Exited = found[0].Pid, false
		if st.Pid <= 0 || !aliveNow(st.Pid) {
			// Its own process is whatever it still holds. Unknown is fine:
			// what matters is that this directory is taken.
			st.Pid = 0
		}
		if st.Started == 0 {
			st.Started = time.Now().Unix()
		}
	}
	w, h := logArea(m.cols, m.rows)
	if w < 1 || h < 1 {
		w, h = 120, 40
	}
	_ = relay.SetSize(dir, w, h)
	s.relay = dir
	s.relayPid, s.relayRead = st.Pid, time.Now()
	s.state, s.code = Running, 0
	s.since = time.Unix(st.Started, 0)
	m.attachLocked(s, w, h, true)
	return true
}

// attachLocked opens a session with no process of its own and starts pouring
// the log into it.
func (m *Module) attachLocked(s *service, w, h int, replay bool) {
	sess, err := session.Attach(session.ID("supervisor/"+s.spec.Name), w, h, m.ctx.Wake)
	if err != nil {
		return
	}
	s.sess = sess
	s.tailStop = make(chan struct{})
	go tail(sess, filepath.Join(s.relay, relay.LogFile), s.tailStop, replay)
}

// tail pours a log into an emulator, and keeps pouring.
//
// from says where to begin: everything, capped, when attaching to a service
// that was already running — the pane should show what it has been saying —
// and nothing when the log was just emptied for a fresh start.
func tail(sess *session.Session, path string, stop <-chan struct{}, replay bool) {
	var offset int64
	if replay {
		if st, err := os.Stat(path); err == nil && st.Size() > replayCap {
			offset = st.Size() - replayCap
		}
	}
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-stop:
			return
		default:
		}
		f, err := os.Open(path)
		if err == nil {
			if _, err := f.Seek(offset, io.SeekStart); err == nil {
				for {
					n, rerr := f.Read(buf)
					if n > 0 {
						sess.Feed(buf[:n])
						offset += int64(n)
					}
					if rerr != nil {
						break
					}
				}
			}
			_ = f.Close()
		}
		// A halved log is shorter than where we were reading; start again.
		if st, err := os.Stat(path); err == nil && st.Size() < offset {
			offset = 0
		}
		select {
		case <-stop:
			return
		case <-time.After(tailInterval):
		}
	}
}

// stopKeptLocked ends a relayed service and everything it started.
//
// The service's group, not the relay: killing the relay hangs up the session
// it leads, which would end the service without ever asking it politely.
func (m *Module) stopKeptLocked(s *service) {
	dir, grace := s.relay, m.grace
	if dir == "" {
		dir = m.relayDir(s.spec.Name)
	}
	m.detachLocked(s)
	s.state, s.since, s.relayPid = Stopped, time.Now(), 0
	go func() {
		killKept(dir, grace)
		if m.ctx.Wake != nil {
			m.ctx.Wake()
		}
	}()
}

// killKept ends a kept service: the process the relay is holding, and the
// relay itself.
//
// Both, by two routes, because either record can be wrong. The file names the
// process to signal, which stops the service the way it expects to be stopped
// — a whole process group, so `npm run dev` takes the server it launched with
// it. The process table names every relay serving this directory, which is the
// only way to reach one whose record was overwritten, and a relay left behind
// still owns the port.
//
// It is what the stop button does and what a restart waits for, because they
// are the same act.
func killKept(dir string, grace time.Duration) {
	if dir == "" {
		return
	}
	if st, ok := relay.ReadState(dir); ok && st.Pid > 0 && aliveNow(st.Pid) {
		_ = syscall.Kill(-st.Pid, syscall.SIGTERM)
		deadline := time.Now().Add(grace)
		for time.Now().Before(deadline) && aliveNow(st.Pid) {
			time.Sleep(25 * time.Millisecond)
		}
		if aliveNow(st.Pid) {
			_ = syscall.Kill(-st.Pid, syscall.SIGKILL)
		}
	}
	stopRelays(dir, grace)
}

// detachLocked lets go of a relay's output without touching the service.
func (m *Module) detachLocked(s *service) {
	if s.tailStop != nil {
		close(s.tailStop)
		s.tailStop = nil
	}
	if s.sess != nil {
		m.captureLocked(s)
		_ = s.sess.Close()
		s.sess = nil
	}
	s.relay = ""
}

// refreshKeptLocked reads what the relay has published. It is the only account
// of a service this application does not hold a process for.
func (m *Module) refreshKeptLocked(s *service) {
	// Throttled, but only once there is something to throttle: until the relay
	// has published which process it is holding, every look is worth taking.
	if s.relayPid > 0 && time.Since(s.relayRead) < relayReadEvery {
		return
	}
	s.relayRead = time.Now()
	st, ok := relay.ReadState(s.relay)
	if ok && st.Pid > 0 {
		s.relayPid = st.Pid
	}
	switch {
	case !ok:
		return
	case st.Exited:
		if s.state != Exited {
			s.state, s.code, s.since = Exited, st.Code, time.Now()
			s.left = stillAlive(s.tree)
		}
	case st.Pid > 0 && !aliveNow(st.Pid):
		if s.state != Exited {
			s.state, s.code, s.since = Exited, -1, time.Now()
			s.left = stillAlive(s.tree)
		}
	}
}

func aliveNow(pid int) bool {
	_, err := os.Stat("/proc/" + itoa(pid))
	return err == nil
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func writeRelayState(dir string, st relay.State) {
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, relay.StateFile), raw, 0o600)
}

// safeName is logName without its extension: one directory a service.
func safeName(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		}
		return '-'
	}, name)
	if safe == "" || strings.Trim(safe, ".-") == "" {
		safe = "service"
	}
	return safe
}
