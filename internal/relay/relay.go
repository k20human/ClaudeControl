// Package relay runs a service on a terminal that outlives the application.
//
// A service kept across a restart cannot be left writing into a pseudo
// terminal nobody reads: the buffer fills — about seventy thousand lines on
// this machine — and the next write blocks for ever. The service then holds
// its port, answers nothing, and says nothing about it, which is worse than
// having been stopped.
//
// So a small process stays behind to drain it. It owns the terminal, the
// service writes to it as it would to any other, and every byte is appended to
// a file exactly as it arrived — escape sequences and all, which is what lets
// the application put the colours back on screen by replaying the file through
// its emulator.
package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Layout of a relay's directory. One directory a service, so that a stale
// relay leaves nothing behind that a new one has to disentangle.
const (
	LogFile   = "log"
	SizeFile  = "size"
	StateFile = "state.json"
)

// LogCap is how much of a service's output is kept. Beyond it the file is
// rewritten from its second half: a development server left running for a week
// must not fill a disk, and the beginning of a log a week old is not what
// anybody opens it for.
const LogCap = 8 << 20

// State is what the relay publishes about itself, for an application that was
// not running when it started.
type State struct {
	Pid     int      `json:"pid"`     // the service, not the relay
	Relay   int      `json:"relay"`   // the process draining the terminal
	Argv    []string `json:"argv"`    //
	Dir     string   `json:"dir"`     //
	Started int64    `json:"started"` // unix seconds
	Exited  bool     `json:"exited"`  //
	Code    int      `json:"code"`    //
}

// ReadState loads what a relay has published, if anything has.
func ReadState(dir string) (State, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, StateFile))
	if err != nil {
		return State{}, false
	}
	var st State
	if json.Unmarshal(raw, &st) != nil {
		return State{}, false
	}
	return st, true
}

// SetSize asks the relay to resize the terminal it owns. Written rather than
// signalled, because the relay may be started by one run of the application
// and resized by another.
func SetSize(dir string, cols, rows int) error {
	if cols < 1 || rows < 1 {
		return fmt.Errorf("relay: bad size %dx%d", cols, rows)
	}
	return os.WriteFile(filepath.Join(dir, SizeFile),
		[]byte(fmt.Sprintf("%d %d\n", cols, rows)), 0o600)
}

// Run is the relay itself: it starts the service, drains its terminal into the
// log, and returns when the service has ended.
//
// Everything it needs is in dir, which the caller has already made.
func Run(dir string, argv []string) error {
	if len(argv) == 0 {
		return errors.New("relay: empty command")
	}
	st, _ := ReadState(dir)

	cols, rows := readSize(dir)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = st.Dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return fmt.Errorf("relay: start: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	st.Pid, st.Relay, st.Argv = cmd.Process.Pid, os.Getpid(), argv
	st.Started, st.Exited, st.Code = time.Now().Unix(), false, 0
	writeState(dir, st)

	var wg sync.WaitGroup
	done := make(chan struct{})

	// The size the application asks for, applied as it changes. A file rather
	// than a signal: the run of the application that resizes a pane is often
	// not the one that started the service.
	wg.Add(1)
	go func() {
		defer wg.Done()
		watchSize(dir, ptmx, cols, rows, done)
	}()

	// The drain. This is the whole point: it never stops until the terminal
	// does, so the service never waits on a buffer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		drain(ptmx, filepath.Join(dir, LogFile))
	}()

	code := 0
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				code = 128 + int(ws.Signal())
			}
		} else {
			code = -1
		}
	}
	close(done)
	_ = ptmx.Close()
	wg.Wait()

	st.Exited, st.Code = true, code
	writeState(dir, st)
	return nil
}

// drain copies the terminal into the log until there is nothing left to copy.
func drain(ptmx *os.File, path string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		// Nowhere to write it, but the terminal still has to be emptied or the
		// service stops on a full buffer. Read and discard.
		_, _ = io.Copy(io.Discard, ptmx)
		return
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 32*1024)
	written := size(f)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr == nil {
				written += int64(n)
				if written > LogCap {
					written = halve(f, path)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// halve rewrites the log from its second half and reports the new length.
//
// A window rather than a rotation: one file a service is what makes reading it
// back simple, and the beginning of a week-old log is not what anybody opens
// it for.
func halve(f *os.File, path string) int64 {
	raw, err := os.ReadFile(path)
	if err != nil {
		return size(f)
	}
	keep := raw[len(raw)/2:]
	// From the start of a line, so the replay never begins mid-sequence.
	if i := indexByte(keep, '\n'); i >= 0 {
		keep = keep[i+1:]
	}
	tmp := path + ".new"
	if os.WriteFile(tmp, keep, 0o600) != nil {
		return size(f)
	}
	if os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
		return size(f)
	}
	// The old handle now points at a file nobody can reach; take a new one.
	if next, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		old := *f
		*f = *next
		_ = old.Close()
	}
	return int64(len(keep))
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func size(f *os.File) int64 {
	if st, err := f.Stat(); err == nil {
		return st.Size()
	}
	return 0
}

// watchSize applies the size the application asks for.
func watchSize(dir string, ptmx *os.File, cols, rows int, done <-chan struct{}) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
			c, r := readSize(dir)
			if c == cols && r == rows {
				continue
			}
			cols, rows = c, r
			_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
		}
	}
}

// readSize is the size the application last asked for, or a sane default. The
// default is wide: a service whose output is wrapped at eighty columns cannot
// be unwrapped later, and a pane narrower than the terminal can still scroll.
func readSize(dir string) (cols, rows int) {
	cols, rows = 120, 40
	raw, err := os.ReadFile(filepath.Join(dir, SizeFile))
	if err != nil {
		return
	}
	var c, r int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(raw)), "%d %d", &c, &r); err != nil {
		return
	}
	if c > 0 && r > 0 {
		cols, rows = c, r
	}
	return
}

func writeState(dir string, st State) {
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, StateFile), raw, 0o600)
}
