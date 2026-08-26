// Package session owns a hosted process: its PTY, its terminal emulator, and
// its lifecycle. A session is independent of where — or whether — it is shown.
package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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

	// OnUpdate, when set, is called after every chunk of process output. The
	// UI uses it to schedule a redraw. It must not block.
	//
	// It belongs to the spec rather than to a settable field: a process can
	// write its first line before the caller gets the Session back, and a
	// callback installed afterwards would miss it — leaving that output on the
	// emulator with nothing to trigger a repaint.
	OnUpdate func()
}

// Session is a hosted process plus the emulator that interprets its output.
type Session struct {
	ID ID

	// Term is the emulated screen. It is safe for concurrent use: the PTY pump
	// writes to it from its own goroutine while the UI reads cells.
	Term vt.Terminal

	onUpdate func()

	ptmx *os.File
	cmd  *exec.Cmd

	drainDone chan struct{}

	// termMu makes a resize atomic against the output pump. Resizing has to
	// read the screen and write it back (see repaintLocked), and the pump
	// must not slip a chunk in between those two steps — it would be painted
	// over by the older snapshot.
	termMu sync.Mutex

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
		onUpdate:  sp.OnUpdate,
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
			s.termMu.Lock()
			_, _ = s.Term.Write(buf[:n])
			s.termMu.Unlock()
			// onUpdate is called outside the lock: it wakes the UI, and the
			// UI resizes sessions.
			if s.onUpdate != nil {
				s.onUpdate()
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
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

// Resize changes both the emulator grid and the PTY window, so the guest gets
// its SIGWINCH.
func (s *Session) Resize(w, h int) error {
	if w < 1 || h < 1 {
		return fmt.Errorf("session: bad size %dx%d", w, h)
	}
	s.termMu.Lock()
	s.Term.Resize(w, h)
	s.repaintLocked()
	s.termMu.Unlock()
	if err := pty.Setsize(s.ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}); err != nil {
		return fmt.Errorf("session: resize: %w", err)
	}
	return nil
}

// repaintLocked writes the screen back onto itself so that it is marked as
// damaged again.
//
// Resizing a vt emulator keeps the buffer but drops every damage mark, and
// Draw only copies lines that are marked. A guest that repaints on SIGWINCH
// — Claude Code, vim, htop — fills the pane back in immediately; one that has
// finished writing, such as a command whose output is just sitting there,
// would leave the pane blank. So the emulator repaints itself from its own
// snapshot.
//
// The clear is what makes this work: writing identical cells back changes
// nothing, and an unchanged cell is not marked. Erasing first guarantees
// every line differs. Render separates lines with a bare newline, so each one
// is positioned explicitly rather than relying on a carriage return, and
// origin mode is turned off so those positions are absolute.
//
// Automatic wrapping is turned off too, and that one is not cosmetic. Painting
// a line that reaches the last column arms the pending-wrap flag, and x/vt's
// DECRC restores the saved position without clearing it — so the guest's very
// next character would drop to the line below, one row off, for ever. With
// wrapping off the flag is never armed. DECSC/DECRC around the whole sequence
// hands back the cursor, pen, character set, origin mode and wrap setting.
//
// Note that Write is used rather than WriteString: on SafeEmulator only Write
// takes the lock, WriteString is promoted from the embedded Emulator.
func (s *Session) repaintLocked() {
	lines := strings.Split(s.Term.Render(), "\n")
	var b strings.Builder
	b.WriteString("\x1b7\x1b[?7l\x1b[?6l\x1b[m\x1b[2J")
	for i, line := range lines {
		fmt.Fprintf(&b, "\x1b[%d;1H%s", i+1, line)
	}
	b.WriteString("\x1b8")
	_, _ = s.Term.Write([]byte(b.String()))
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
