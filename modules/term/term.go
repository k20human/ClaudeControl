// Package term hosts a process in a pane. It is the module type a Claude Code
// session uses.
package term

import (
	"fmt"
	"os"
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
		if m.ctx.Sessions != nil {
			m.ctx.Sessions.Add(s)
		}
		return nil
	}
	return m.sess.Resize(w, h)
}

// Draw paints the emulated screen into area.
func (m *Module) Draw(scr uv.Screen, area uv.Rectangle) {
	if m.sess == nil {
		return
	}
	m.sess.Term.Draw(scr, area)
}

// Cursor reports the guest cursor, pane-local.
func (m *Module) Cursor() (x, y int, visible bool) {
	if m.sess == nil {
		return 0, 0, false
	}
	p := m.sess.Term.CursorPosition()
	return p.X, p.Y, true
}

// Key forwards a key press, re-encoded for the guest.
func (m *Module) Key(k uv.KeyEvent) {
	if m.sess != nil {
		m.sess.SendKey(k)
	}
}

// Mouse forwards a pane-local mouse event, re-encoded for the guest.
func (m *Module) Mouse(e uv.MouseEvent) {
	if m.sess != nil {
		m.sess.SendMouse(e)
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

// Close stops the process and drops it from the registry.
func (m *Module) Close() error {
	if m.sess == nil {
		return nil
	}
	if m.ctx.Sessions != nil {
		m.ctx.Sessions.Remove(m.sess.ID)
	}
	return m.sess.Close()
}
