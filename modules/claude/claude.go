// Package claude hosts a Claude Code session. It is the term module with the
// two flags that make a session identifiable and observable.
package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/hooks"
	"claudecontrol/internal/module"
	"claudecontrol/internal/session"
	"claudecontrol/internal/uuid"
)

func init() { module.Register("claude", New) }

// Module hosts one Claude Code session.
type Module struct {
	// view is the window onto the session: usually the live screen, and
	// sometimes further back when the wheel has been turned.
	view session.View

	binary string
	dir    string
	extra  []string
	resume string

	ctx  module.Context
	id   string
	sess *session.Session
}

// New builds a claude module. Recognised keys: "bin" (defaults to "claude"),
// "dir", "args" (a list of extra arguments) and "resume" (a session id).
func New(cfg map[string]any) (module.Module, error) {
	m := &Module{binary: "claude"}
	if v, ok := cfg["bin"].(string); ok && v != "" {
		m.binary = v
	}
	if v, ok := cfg["dir"].(string); ok {
		m.dir = expand(v)
	}
	if v, ok := cfg["resume"].(string); ok {
		m.resume = v
	}
	switch v := cfg["args"].(type) {
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("claude: %q must hold strings, got %T", "args", item)
			}
			m.extra = append(m.extra, s)
		}
	case []string:
		m.extra = append(m.extra, v...)
	case nil:
	default:
		return nil, fmt.Errorf("claude: %q must be a list, got %T", "args", v)
	}
	return m, nil
}

// expand resolves a leading ~ as well as environment variables, because a
// configuration file is written by hand and "~/DEV" is what a hand writes.
func expand(p string) string {
	p = os.ExpandEnv(p)
	if p == "~" || (len(p) > 1 && p[:2] == "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// Argv assembles the command line.
//
// Resuming and imposing an identity are mutually exclusive: --resume adopts the
// old session's id, so asking for a new one at the same time would be asking
// Claude Code for two different things at once.
func Argv(binary, sessionID, resume string, extra []string) []string {
	out := []string{binary}
	if resume != "" {
		out = append(out, "--resume", resume)
	} else {
		out = append(out, "--session-id", sessionID)
	}
	return append(out, extra...)
}

// Init records the context and settles on a working directory.
func (m *Module) Init(ctx module.Context) error {
	m.ctx = ctx
	if m.dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("claude: working directory: %w", err)
		}
		m.dir = wd
	}
	id, err := uuid.New()
	if err != nil {
		return err
	}
	m.id = id
	return nil
}

// SessionID is the identity imposed on, or adopted by, this session.
func (m *Module) SessionID() string {
	if m.resume != "" {
		return m.resume
	}
	return m.id
}

// Resize starts the session on first call, then propagates the new size.
func (m *Module) Resize(w, h int) error {
	if w < 1 || h < 1 {
		return nil
	}
	if m.sess != nil {
		return m.sess.Resize(w, h)
	}

	argv := Argv(m.binary, m.id, m.resume, m.extra)
	var env []string
	if m.ctx.HookSocket != "" && m.ctx.Binary != "" {
		settings, err := hooks.SettingsJSON(m.ctx.Binary, m.ctx.HookSocket)
		if err != nil {
			return err
		}
		argv = append(argv, "--settings", settings)
		env = append(env, hooks.EnvSocket+"="+m.ctx.HookSocket)
	}

	s, err := session.Start(session.Spec{
		ID:       session.ID(m.SessionID()),
		Argv:     argv,
		Dir:      m.dir,
		Env:      env,
		Width:    w,
		Height:   h,
		OnUpdate: m.ctx.Wake,
	})
	if err != nil {
		return err
	}
	m.sess = s
	if m.ctx.Pool != nil {
		m.ctx.Pool.Add(s, m.title(), m.dir)
		m.ctx.Pool.SetAttached(s.ID, true)
	}
	return nil
}

// title is the short name the sessions list shows: the last element of the
// working directory, which is what tells two sessions apart at a glance.
// Title names the pane after the directory the session runs in. The
// application replaces it with the name Claude Code gave the session once one
// has been seen, which is what a person recognises it by.
func (m *Module) Title() (string, bool) { return m.title(), true }

func (m *Module) title() string {
	if base := filepath.Base(m.dir); base != "" && base != "." && base != "/" {
		return base
	}
	return "claude " + strconv.FormatUint(uint64(m.ctx.PaneID), 10)
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

// Key forwards a key press. Printable characters bypass the emulator's key
// encoder, which drops anything carrying a modifier it does not know — every
// capital letter included.
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

// Mouse forwards a pane-local mouse event.
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

// Session exposes the hosted session.
func (m *Module) Session() *session.Session { return m.sess }

// ScrollOffset is how far back the view is, zero being the live screen.
func (m *Module) ScrollOffset() int { return m.view.Offset() }

// Close releases the pane's hold on the session.
//
// It detaches rather than kills: a Claude session holds a conversation, and
// closing a pane is a decision about the screen. The term module does the
// opposite, because a shell has nothing worth keeping. Ending a Claude session
// is the pool's decision, taken from the sessions list.
func (m *Module) Close() error {
	if m.sess == nil {
		return nil
	}
	if m.ctx.Pool != nil {
		m.ctx.Pool.SetAttached(m.sess.ID, false)
	}
	m.sess = nil
	return nil
}
