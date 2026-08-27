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
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
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

	// Callbacks are added to the ones the session installs for itself. They
	// are given here rather than set afterwards because SetCallbacks replaces
	// the whole set — a caller installing its own would silently drop the
	// session's — and because it is promoted from the unguarded type, so it
	// can only be called before the pump starts.
	Callbacks vt.Callbacks
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

	// motion records whether the guest asked to be told about the pointer
	// moving, which decides who a drag belongs to. Guarded because the
	// emulator reports mode changes from the pump's goroutine.
	motionMu sync.RWMutex
	motion   bool

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

	// Installed before the pump starts, which is the only safe moment.
	cb := sp.Callbacks
	theirEnable, theirDisable := cb.EnableMode, cb.DisableMode
	cb.EnableMode = func(m ansi.Mode) {
		s.noteMode(m, true)
		if theirEnable != nil {
			theirEnable(m)
		}
	}
	cb.DisableMode = func(m ansi.Mode) {
		s.noteMode(m, false)
		if theirDisable != nil {
			theirDisable(m)
		}
	}
	s.Term.SetCallbacks(cb)

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

// History is how many lines have scrolled off the top and been kept.
func (s *Session) History() int {
	s.termMu.Lock()
	defer s.termMu.Unlock()
	return s.Term.ScrollbackLen()
}

// AltScreen reports whether the guest has taken the whole screen for itself.
//
// It decides who the wheel belongs to. A guest on the alternate screen is
// drawing its own view and scrolling it its own way — there is no history
// behind it, because nothing has scrolled off. A guest on the normal screen
// has pushed its past into the scrollback, and that past is what the wheel
// should reach.
func (s *Session) AltScreen() bool {
	s.termMu.Lock()
	defer s.termMu.Unlock()
	return s.Term.IsAltScreen()
}

// DrawScrolled paints the session with offset lines of history above the live
// screen.
//
// At offset zero this is the ordinary path: Draw, which copies only the lines
// the emulator considers damaged and is what keeps a busy pane cheap. Any
// other offset takes the slow path and copies every visible cell, because the
// mapping from emulator row to screen row has changed and damage marks are
// about rows that have not moved. Scrolling is a transient state, and a full
// copy of one pane for as long as it lasts is a fair price for not having to
// reason about which of two coordinate systems a damage mark belongs to.
func (s *Session) DrawScrolled(scr uv.Screen, area uv.Rectangle, offset int) {
	if offset <= 0 {
		s.Term.Draw(scr, area)
		return
	}

	s.termMu.Lock()
	defer s.termMu.Unlock()

	history := s.Term.ScrollbackLen()
	if offset > history {
		offset = history
	}
	if offset <= 0 {
		s.Term.Draw(scr, area)
		return
	}

	for row := 0; row < area.Dy(); row++ {
		for col := 0; col < area.Dx(); col++ {
			// The cells are read and copied while the lock is held, which is
			// what makes this safe: these accessors hand back a pointer into
			// the live buffer, and the pump writes to that buffer under the
			// same lock.
			var cell *uv.Cell
			if row < offset {
				cell = s.Term.ScrollbackCellAt(col, history-offset+row)
			} else {
				cell = s.Term.CellAt(col, row-offset)
			}
			if cell == nil {
				scr.SetCell(area.Min.X+col, area.Min.Y+row, &uv.EmptyCell)
				continue
			}
			copied := *cell
			scr.SetCell(area.Min.X+col, area.Min.Y+row, &copied)
		}
	}
}

// noteMode records the mouse modes that matter to us.
//
// Only the two that report the pointer moving. A guest that has asked for them
// is doing something with a drag; one that has not cannot see a drag at all,
// which is what makes it safe to use the gesture for something else.
func (s *Session) noteMode(m ansi.Mode, on bool) {
	switch m.Mode() {
	case ansi.MouseCellMotionMode.Mode(), ansi.MouseAllMotionMode.Mode():
	default:
		return
	}
	s.motionMu.Lock()
	s.motion = on
	s.motionMu.Unlock()
}

// TracksMotion reports whether the guest asked to be told about the pointer
// moving. It decides who a drag belongs to.
//
// Claude Code asks for button events and nothing more, so a drag inside its
// pane reaches it as a press and a release with nothing in between — it cannot
// see the drag, and the gesture is free for selecting text. A full-screen
// editor with the mouse enabled does ask, and keeps its drag.
func (s *Session) TracksMotion() bool {
	s.motionMu.RLock()
	defer s.motionMu.RUnlock()
	return s.motion
}

// FindLines lists the lines of the whole output, history and screen together,
// that contain the query — as absolute line indices, the same ones the view
// and the selection use.
//
// Case is ignored, because you are looking for something you half remember
// rather than matching a pattern.
func (s *Session) FindLines(query string) []int {
	if query == "" {
		return nil
	}
	want := strings.ToLower(query)

	s.termMu.Lock()
	defer s.termMu.Unlock()

	history := s.Term.ScrollbackLen()
	height := s.Term.Bounds().Dy()
	width := s.Term.Bounds().Dx()

	var out []int
	var b strings.Builder
	for line := 0; line < history+height; line++ {
		b.Reset()
		for col := 0; col < width; col++ {
			var cell *uv.Cell
			if line < history {
				cell = s.Term.ScrollbackCellAt(col, line)
			} else {
				cell = s.Term.CellAt(col, line-history)
			}
			if cell == nil || cell.Content == "" {
				b.WriteByte(' ')
				continue
			}
			b.WriteString(cell.Content)
		}
		if strings.Contains(strings.ToLower(b.String()), want) {
			out = append(out, line)
		}
	}
	return out
}

// LineText reads part of one line of the whole output, history and screen
// together, as text.
//
// Under the lock for the same reason the scrolled drawing is: these accessors
// hand back a pointer into the live buffer, and the pump writes to that buffer
// under the same lock.
func (s *Session) LineText(line, from, to int) string {
	s.termMu.Lock()
	defer s.termMu.Unlock()

	history := s.Term.ScrollbackLen()
	width := s.Term.Bounds().Dx()
	if to >= width {
		to = width - 1
	}
	if from < 0 {
		from = 0
	}

	var b strings.Builder
	for col := from; col <= to; col++ {
		var cell *uv.Cell
		if line < history {
			cell = s.Term.ScrollbackCellAt(col, line)
		} else {
			cell = s.Term.CellAt(col, line-history)
		}
		if cell == nil || cell.Content == "" {
			b.WriteByte(' ')
			continue
		}
		b.WriteString(cell.Content)
	}
	return b.String()
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

// Pid is the process id, or zero once it is gone.
func (s *Session) Pid() int {
	if s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// Terminate asks the process to stop, and insists after grace has passed.
//
// The signal goes to the whole process group, not to the child alone. A PTY is
// started with its own session, so the child leads a group its own children
// join — and `npm run dev` is a launcher whose real server is one of those
// children. Signalling only the child leaves the server holding its port,
// which is the failure that makes a supervisor useless.
//
// SIGTERM first because a development server asked politely closes its sockets
// and removes its lockfiles; SIGKILL after, because one that ignores the
// request must still go.
func (s *Session) Terminate(grace time.Duration) error {
	pid := s.Pid()
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		// Already gone, or never a group leader: fall back on the blunt path
		// rather than leaving it running.
		return s.Close()
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if st, _ := s.Status(); st == Exited {
			return s.Close()
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	return s.Close()
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
