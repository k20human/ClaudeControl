// Package session owns a hosted process: its PTY, its terminal emulator, and
// its lifecycle. A session is independent of where — or whether — it is shown.
package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// ID identifies a session for its whole life.
type ID string

// Status is a session's coarse lifecycle state.
type Status int

const (
	// Running means the process is alive.
	Running Status = iota
	// Exited means the process is gone; the exit code is meaningful.
	Exited
)

// Spec describes a session to start.
type Spec struct {
	ID     ID
	Argv   []string
	Dir    string
	Env    []string // appended to the parent environment
	Width  int
	Height int
}

// Session is a hosted process plus the emulator that interprets its output.
type Session struct {
	ID ID

	// Term is the emulated screen. It is safe for concurrent use: the PTY pump
	// writes to it from its own goroutine while the UI reads cells.
	Term vt.Terminal

	// OnUpdate, when set, is called after every chunk of process output. The
	// UI uses it to schedule a redraw. It must not block.
	OnUpdate func()

	ptmx *os.File
	cmd  *exec.Cmd

	drainDone chan struct{}

	mu       sync.Mutex
	status   Status
	exitCode int
	closed   bool
}

// Start launches the process on a new PTY sized w x h.
func Start(sp Spec) (*Session, error) {
	if len(sp.Argv) == 0 {
		return nil, errors.New("session: empty command")
	}
	if sp.Width < 1 || sp.Height < 1 {
		return nil, fmt.Errorf("session: bad size %dx%d", sp.Width, sp.Height)
	}

	cmd := exec.Command(sp.Argv[0], sp.Argv[1:]...)
	cmd.Dir = sp.Dir
	// TERM must advertise a terminal x/vt actually emulates, otherwise the
	// guest falls back to a crippled feature set.
	cmd.Env = append(append(os.Environ(), "TERM=xterm-256color"), sp.Env...)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(sp.Height),
		Cols: uint16(sp.Width),
	})
	if err != nil {
		return nil, fmt.Errorf("session: start: %w", err)
	}

	s := &Session{
		ID:        sp.ID,
		Term:      vt.NewSafeEmulator(sp.Width, sp.Height),
		ptmx:      ptmx,
		cmd:       cmd,
		drainDone: make(chan struct{}),
	}

	// Process output feeds the emulator.
	go s.pumpOutput()
	// Emulator replies — query answers, and everything pushed by SendKey and
	// friends — go back to the process.
	go func() {
		defer close(s.drainDone)
		_, _ = io.Copy(ptmx, s.Term)
	}()

	return s, nil
}

// pumpOutput copies the PTY to the emulator until the process ends.
func (s *Session) pumpOutput() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			_, _ = s.Term.Write(buf[:n])
			if s.OnUpdate != nil {
				s.OnUpdate()
			}
		}
		if err != nil {
			break
		}
	}
	code := 0
	if err := s.cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	s.mu.Lock()
	s.status = Exited
	s.exitCode = code
	s.mu.Unlock()
	if s.OnUpdate != nil {
		s.OnUpdate()
	}
}

// Resize changes both the emulator grid and the PTY window, so the guest gets
// its SIGWINCH.
func (s *Session) Resize(w, h int) error {
	if w < 1 || h < 1 {
		return fmt.Errorf("session: bad size %dx%d", w, h)
	}
	s.Term.Resize(w, h)
	if err := pty.Setsize(s.ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}); err != nil {
		return fmt.Errorf("session: resize: %w", err)
	}
	return nil
}

// SendKey encodes a key the way this guest asked for it.
func (s *Session) SendKey(k uv.KeyEvent) { s.Term.SendKey(k) }

// SendMouse encodes a mouse event the way this guest asked for it. Coordinates
// must already be pane-local.
func (s *Session) SendMouse(m uv.MouseEvent) { s.Term.SendMouse(m) }

// SendText writes raw text to the guest.
func (s *Session) SendText(text string) { s.Term.SendText(text) }

// Paste writes text, bracketed if the guest enabled bracketed paste.
func (s *Session) Paste(text string) { s.Term.Paste(text) }

// Status reports the lifecycle state and, once exited, the exit code.
func (s *Session) Status() (Status, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.exitCode
}

// Close kills the process and releases the PTY. It is idempotent.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	err := s.ptmx.Close()

	// Stop the reply drain, or every closed pane leaks a goroutine.
	//
	// The emulator is never closed on purpose. vt.SafeEmulator leaves Read
	// unlocked — it has to, since Read blocks on an internal pipe — and
	// inherits Close unguarded from the embedded Emulator. Both touch the same
	// closed flag, so closing while the drain sits in Read is a data race
	// inside the library. Leaving the flag alone removes the race entirely;
	// the emulator holds no operating-system resource, only memory the garbage
	// collector reclaims once the drain is gone.
	//
	// The drain is woken with one byte: its next write fails against the
	// already-closed PTY and io.Copy returns. The nudge runs in its own
	// goroutine because a drain that left through a write error is no longer
	// reading, and the pipe write would then block forever.
	go func() { s.Term.SendText("\x00") }()
	select {
	case <-s.drainDone:
	case <-time.After(time.Second):
	}

	if err != nil && !errors.Is(err, os.ErrClosed) {
		return fmt.Errorf("session: close: %w", err)
	}
	return nil
}
