// Package tasks runs the commands you run often, in a pane of their own.
package tasks

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/module"
	"claudecontrol/internal/render"
	"claudecontrol/internal/session"
)

func init() { module.Register("tasks", New) }

var (
	bgPanel    = color.RGBA{R: 0x14, G: 0x1a, B: 0x24, A: 0xff}
	fgName     = color.RGBA{R: 0xc7, G: 0xd2, B: 0xe0, A: 0xff}
	fgHint     = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	bgSelected = color.RGBA{R: 0x24, G: 0x2e, B: 0x3d, A: 0xff}
)

var seq atomic.Uint64

// Task is one command worth a name.
type Task struct {
	Name string
	Argv []string
	Dir  string
}

// Module lists the tasks and runs the selected one.
type Module struct {
	ctx      module.Context
	tasks    []Task
	selected int
	w, h     int

	// running is the task currently shown, if any. A task is hosted like any
	// other command, which is what puts its output on screen and its entry in
	// the sessions list.
	running *session.Session

	// view is how that output is looked at: the wheel reaches its history and
	// the pane search runs over it. A task prints a great deal, and the line
	// that matters is rarely the last one.
	view session.View
}

// New builds the module from its configuration.
func New(cfg map[string]any) (module.Module, error) {
	m := &Module{}
	raw, _ := cfg["tasks"].([]any)
	for _, item := range raw {
		spec, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tasks: each task must be a mapping, got %T", item)
		}
		name, _ := spec["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("tasks: a task has no %q", "name")
		}
		argv, ok := toStrings(spec["cmd"])
		if !ok || len(argv) == 0 {
			return nil, fmt.Errorf("tasks: task %q has no %q", name, "cmd")
		}
		dir, _ := spec["dir"].(string)
		m.tasks = append(m.tasks, Task{Name: name, Argv: argv, Dir: expand(dir)})
	}
	return m, nil
}

// expand resolves a leading ~ as well as environment variables, because a
// configuration file is written by hand.
func expand(p string) string {
	if p == "" {
		return ""
	}
	p = os.ExpandEnv(p)
	if p == "~" || (len(p) > 1 && p[:2] == "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// Init records the context.
func (m *Module) Init(ctx module.Context) error {
	m.ctx = ctx
	return nil
}

// Tasks are the configured commands.
func (m *Module) Tasks() []Task { return m.tasks }

// Selected is the highlighted task.
func (m *Module) Selected() (Task, bool) {
	if m.selected < 0 || m.selected >= len(m.tasks) {
		return Task{}, false
	}
	return m.tasks[m.selected], true
}

// MoveSelection moves the highlight, stopping at either end.
func (m *Module) MoveSelection(delta int) {
	m.selected += delta
	if m.selected >= len(m.tasks) {
		m.selected = len(m.tasks) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// Run starts the selected task, replacing whatever ran before it.
func (m *Module) Run() error {
	task, ok := m.Selected()
	if !ok {
		return nil
	}
	if m.w < 1 || m.h < 1 {
		return fmt.Errorf("tasks: the pane has no size yet")
	}
	m.stopRunning()

	dir := task.Dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("tasks: working directory: %w", err)
		}
		dir = wd
	}
	id := session.ID("task-" + task.Name + "-" + strconv.FormatUint(seq.Add(1), 10))
	s, err := session.Start(session.Spec{
		ID: id, Argv: task.Argv, Dir: dir,
		Width: m.w, Height: m.h, OnUpdate: m.ctx.Wake,
	})
	if err != nil {
		return err
	}
	m.running = s
	// The offset and the search belonged to output that no longer exists.
	m.view.Reset()
	m.view.FindClear()
	if m.ctx.Pool != nil {
		m.ctx.Pool.Add(s, task.Name, dir)
		m.ctx.Pool.SetAttached(s.ID, true)
	}
	return nil
}

// stopRunning ends the task on screen, if there is one.
func (m *Module) stopRunning() {
	if m.running == nil {
		return
	}
	if m.ctx.Pool != nil {
		_ = m.ctx.Pool.Kill(m.running.ID)
	} else {
		_ = m.running.Close()
	}
	m.running = nil
}

// Resize records the size and passes it on to a running task.
func (m *Module) Resize(w, h int) error {
	m.w, m.h = w, h
	if m.running != nil {
		return m.running.Resize(w, h)
	}
	return nil
}

// Draw shows the running task if there is one, and the list otherwise.
//
// One pane, two states: a task you are about to run and a task you are
// watching are the same thing at different moments, and giving them separate
// panes would leave one of them empty most of the time.
func (m *Module) Draw(scr uv.Screen, area uv.Rectangle) {
	if m.running != nil {
		m.view.Draw(m.running, scr, area)
		return
	}
	render.Fill(scr, area, bgPanel)
	if len(m.tasks) == 0 {
		render.Text(scr, area.Min.X, area.Min.Y, "no tasks configured", fgHint, bgPanel)
		return
	}
	for i, task := range m.tasks {
		y := area.Min.Y + i
		if y >= area.Max.Y-1 {
			break
		}
		bg := color.Color(bgPanel)
		if i == m.selected {
			bg = bgSelected
			render.Fill(scr, uv.Rect(area.Min.X, y, area.Dx(), 1), bg)
		}
		line := task.Name + "  " + strings.Join(task.Argv, " ")
		if ansi.StringWidth(line) > area.Dx() {
			line = ansi.Truncate(line, area.Dx(), "")
		}
		render.Text(scr, area.Min.X, y, line, fgName, bg)
	}
	if area.Dy() > 1 {
		render.Text(scr, area.Min.X, area.Max.Y-1, "↑↓ select   ⏎ run", fgHint, bgPanel)
	}
}

// Cursor puts the cursor in a running task, so an interactive one can be typed
// into.
func (m *Module) Cursor() (x, y int, visible bool) {
	if m.running == nil {
		return 0, 0, false
	}
	p := m.running.Term.CursorPosition()
	return p.X, p.Y, true
}

// Key either drives the list or reaches a running task.
func (m *Module) Key(k uv.KeyEvent) {
	if m.running != nil {
		if key := k.Key(); key.Text != "" {
			m.running.SendText(key.Text)
			return
		}
		m.running.SendKey(k)
		return
	}
	key := k.Key()
	switch {
	case key.Code == uv.KeyUp || key.Text == "k":
		m.MoveSelection(-1)
	case key.Code == uv.KeyDown || key.Text == "j":
		m.MoveSelection(1)
	case key.Code == uv.KeyEnter:
		_ = m.Run()
	}
}

// Mouse forwards to a running task; the list is driven from the keyboard.
//
// The wheel is the exception: a task's output is a thing you read back, and
// hosting it took the terminal's own scrollbar away.
func (m *Module) Mouse(e uv.MouseEvent) {
	if m.running == nil {
		return
	}
	if wheel, ok := e.(uv.MouseWheelEvent); ok {
		switch wheel.Button {
		case uv.MouseWheelUp:
			m.view.Wheel(m.running, true)
		case uv.MouseWheelDown:
			m.view.Wheel(m.running, false)
		default:
			return
		}
		if m.ctx.Wake != nil {
			m.ctx.Wake()
		}
		return
	}
	m.running.SendMouse(e)
}

// Paste forwards to a running task.
func (m *Module) Paste(text string) {
	if m.running != nil {
		m.running.Paste(text)
	}
}

// Close stops whatever is running.
func (m *Module) Close() error {
	m.stopRunning()
	return nil
}

// toStrings reads a command line from the configuration.
//
// Scalars are converted rather than refused. YAML reads a bare true as a
// boolean and a bare 6379 as a number, so "cmd: [true]" and
// "cmd: [redis-cli, ping, 6379]" would otherwise fail for a reason that has
// nothing to do with what the user wrote. An argument is text by nature.
func toStrings(v any) ([]string, bool) {
	switch list := v.(type) {
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
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
	case []string:
		return list, true
	}
	return nil, false
}
