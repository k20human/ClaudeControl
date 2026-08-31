// Package term hosts a process in a pane. It is the module type a Claude Code
// session uses.
package term

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/module"
	"claudecontrol/internal/session"
)

func init() { module.Register("term", New) }

var seq atomic.Uint64

// Module hosts one process. It is a thin adapter: x/vt already knows how to
// draw itself into a uv.Screen and how to encode input for the guest.
type Module struct {
	// view is the window onto the session: usually the live screen, and
	// sometimes further back when the wheel has been turned.
	view session.View

	argv []string
	dir  string

	ctx  module.Context
	sess *session.Session
	w, h int
}

// New builds a term module. Recognised keys: "cmd" (list of strings, required)
// and "dir" (string, defaults to the process working directory).
func New(cfg map[string]any) (module.Module, error) {
	m := &Module{}
	raw, ok := cfg["cmd"]
	if !ok {
		return nil, fmt.Errorf("term: missing %q", "cmd")
	}
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("term: %q must hold strings, got %T", "cmd", item)
			}
			m.argv = append(m.argv, s)
		}
	case []string:
		m.argv = append(m.argv, v...)
	case string:
		m.argv = []string{v}
	default:
		return nil, fmt.Errorf("term: %q must be a string or a list, got %T", "cmd", raw)
	}
	if len(m.argv) == 0 {
		return nil, fmt.Errorf("term: %q is empty", "cmd")
	}
	if d, ok := cfg["dir"].(string); ok {
		m.dir = os.ExpandEnv(d)
	}
	return m, nil
}

// Init records the context. The process starts on the first Resize, because a
// PTY cannot be opened before its size is known.
func (m *Module) Init(ctx module.Context) error {
	m.ctx = ctx
	if m.dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("term: working directory: %w", err)
		}
		m.dir = wd
	}
	return nil
}

// Resize starts the session on first call, then propagates the new size.
func (m *Module) Resize(w, h int) error {
	if w < 1 || h < 1 {
		return nil
	}
	m.w, m.h = w, h
	if m.sess == nil {
		id := session.ID("pane-" + strconv.FormatUint(uint64(m.ctx.PaneID), 10) +
			"-" + strconv.FormatUint(seq.Add(1), 10))
		s, err := session.Start(session.Spec{
			ID: id, Argv: m.argv, Dir: m.dir, Width: w, Height: h,
			OnUpdate: m.ctx.Wake,
		})
		if err != nil {
			return err
		}
		m.sess = s
		if m.ctx.Pool != nil {
			m.ctx.Pool.Add(s, m.title(), m.dir)
			// Marking it attached is not decoration: the list distinguishes a
			// session on screen from one running in the background, and a pane
			// that adds without attaching makes the list say the opposite of
			// what is true.
			m.ctx.Pool.SetAttached(s.ID, true)
		}
		return nil
	}
	return m.sess.Resize(w, h)
}

// SessionID is the identity this pane's process was given, or empty before it
// has started. It is what lets the pane title and a tab strip say what the
// session is doing: the pool knows the state, and this is the key to it.
func (m *Module) SessionID() string {
	if m.sess == nil {
		return ""
	}
	return string(m.sess.ID)
}

// title is what the sessions list shows. The pane number is part of it because
// several panes commonly run the same command in the same directory, and rows
// nobody can tell apart are rows nobody can act on.
func (m *Module) title() string {
	name := filepath.Base(m.argv[0])
	return name + " " + strconv.FormatUint(uint64(m.ctx.PaneID), 10)
}

// Draw paints the emulated screen into area.
func (m *Module) Draw(scr uv.Screen, area uv.Rectangle) {
	m.view.Draw(m.sess, scr, area)
}

// Cursor reports the guest cursor, pane-local.
func (m *Module) Cursor() (x, y int, visible bool) {
	if m.sess == nil {
		return 0, 0, false
	}
	p := m.sess.Term.CursorPosition()
	return p.X, p.Y, true
}

// Key forwards a key press to the guest.
//
// Printable characters are sent as text rather than through the emulator's key
// encoder. That encoder compares whole key structs and ends with
//
//	default:
//		if key.Mod == 0 { seq += string(key.Code) }
//
// so it emits nothing at all for a shifted key — and ultraviolet reports "A" as
// {Text:"A", Mod:ModShift, Code:'a'}. Routed through it, every capital letter
// and every shifted symbol would vanish silently.
//
// Keys with no text — arrows, function keys, Ctrl combinations, Alt
// combinations — still go through the encoder, which is where it earns its
// keep: it answers with the sequence this guest asked for, honouring the
// application-cursor and application-keypad modes it negotiated.
func (m *Module) Key(k uv.KeyEvent) {
	if m.sess == nil {
		return
	}
	// Typing returns to the bottom, the way a terminal jumps back to the
	// prompt: what you type appears where the cursor is, and you should be
	// looking at it.
	m.view.Reset()
	if key := k.Key(); key.Text != "" {
		m.sess.SendText(key.Text)
		return
	}
	m.sess.SendKey(k)
}

// Mouse forwards a pane-local mouse event, re-encoded for the guest.
func (m *Module) Mouse(e uv.MouseEvent) {
	if m.sess == nil {
		return
	}
	// A guest that follows the pointer is doing something with every gesture,
	// and gets them all.
	if m.sess.TracksMotion() {
		m.sess.SendMouse(e)
		return
	}

	switch ev := e.(type) {
	case uv.MouseWheelEvent:
		// The wheel reaches the history first, unless the guest has taken the
		// whole screen and is drawing its own.
		if m.view.Wheel(m.sess, ev.Button == uv.MouseWheelUp) {
			m.wake()
			return
		}

	case uv.MouseClickEvent:
		if ev.Button == uv.MouseLeft {
			// A press is a click until it moves. The guest gets it now,
			// because a guest that asked for button events is entitled to
			// them, and is owed a release later if this turns into a drag.
			m.view.ClearSelection()
			m.view.Press(m.sess, ev.X, ev.Y, true)
			m.sess.SendMouse(e)
			m.wake()
			return
		}

	case uv.MouseMotionEvent:
		if ev.Button == uv.MouseLeft {
			first := !m.view.Dragging()
			m.view.Drag(m.sess, ev.X, ev.Y)
			if first && m.view.PressForwarded() {
				// It was told the button went down and must not be left
				// believing it still is.
				m.sess.SendMouse(uv.MouseReleaseEvent(uv.Mouse{
					X: ev.X, Y: ev.Y, Button: uv.MouseLeft,
				}))
			}
			m.wake()
			return
		}

	case uv.MouseReleaseEvent:
		// The guest was already released when the drag began.
		if m.view.Release() {
			m.wake()
			return
		}
	}
	m.sess.SendMouse(e)
}

// Find looks for a query in this pane's whole output and moves to the nearest
// match, reporting how many there are.
func (m *Module) Find(query string) int { return m.view.Find(m.sess, query) }

// FindNext moves through the matches, wrapping.
func (m *Module) FindNext(delta int) { m.view.FindNext(m.sess, delta) }

// FindClear forgets the query, leaving the view where the search left it.
func (m *Module) FindClear() { m.view.FindClear() }

// FindStatus is the query, which match you are on, and how many there are.
func (m *Module) FindStatus() (string, int, int) { return m.view.FindStatus() }

// SelectedText is what is selected in this pane, or empty.
func (m *Module) SelectedText() string { return m.view.SelectedText(m.sess) }

func (m *Module) wake() {
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

// Paste forwards pasted text without inspecting it.
func (m *Module) Paste(text string) {
	if m.sess != nil {
		m.sess.Paste(text)
	}
}

// Session exposes the hosted session so the application can read its status.
func (m *Module) Session() *session.Session { return m.sess }

// Close ends the process.
//
// A term pane owns what it hosts: a shell has no conversation worth keeping,
// so closing the pane closes the command. The claude module does the opposite
// and merely detaches, because a Claude session does have something to keep.
// ScrollOffset is how far back the view is, zero being the live screen.
func (m *Module) ScrollOffset() int { return m.view.Offset() }

func (m *Module) Close() error {
	if m.sess == nil {
		return nil
	}
	if m.ctx.Pool != nil {
		return m.ctx.Pool.Kill(m.sess.ID)
	}
	return m.sess.Close()
}

// Dir is where this pane is working, which is where a tab opened from beside
// it should open too.
func (m *Module) Dir() string { return m.dir }
