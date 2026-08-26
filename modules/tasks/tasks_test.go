package tasks_test

import (
	"testing"
	"time"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/modules/tasks"
)

func build(t *testing.T, cfg map[string]any) (module.Module, *pool.Pool) {
	t.Helper()
	m, err := module.New("tasks", cfg)
	if err != nil {
		t.Fatalf("module.New: %v", err)
	}
	b := bus.New()
	p := pool.New(b)
	if err := m.Init(module.Context{PaneID: 1, Pool: p, Bus: b, Wake: func() {}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = m.Close(); _ = p.CloseAll() })
	return m, p
}

func sample(dir string) map[string]any {
	return map[string]any{"tasks": []any{
		map[string]any{"name": "quick", "cmd": []any{"sleep", "5"}, "dir": dir},
		map[string]any{"name": "noisy", "cmd": []any{"sh", "-c", "echo hello"}, "dir": dir},
	}}
}

func TestTasksAreReadFromTheConfiguration(t *testing.T) {
	m, _ := build(t, sample(t.TempDir()))
	lister, ok := m.(interface{ Tasks() []tasks.Task })
	if !ok {
		t.Fatal("the tasks module does not expose its tasks")
	}
	got := lister.Tasks()
	if len(got) != 2 || got[0].Name != "quick" || got[1].Name != "noisy" {
		t.Fatalf("Tasks = %+v, want quick then noisy", got)
	}
}

func TestSelectionStopsAtBothEnds(t *testing.T) {
	m, _ := build(t, sample(t.TempDir()))
	sel := m.(interface {
		Selected() (tasks.Task, bool)
		MoveSelection(int)
	})
	sel.MoveSelection(-5)
	if got, _ := sel.Selected(); got.Name != "quick" {
		t.Errorf("Selected = %q past the start, want quick", got.Name)
	}
	sel.MoveSelection(5)
	if got, _ := sel.Selected(); got.Name != "noisy" {
		t.Errorf("Selected = %q past the end, want noisy", got.Name)
	}
}

// Running a task puts it in the pool, so it appears in the sessions list like
// anything else the application hosts.
func TestRunningATaskPutsItInThePool(t *testing.T) {
	m, p := build(t, sample(t.TempDir()))
	runner := m.(interface{ Run() error })
	if err := m.Resize(40, 6); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := runner.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range p.All() {
			if e.Title == "quick" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the task is not in the pool; it holds %d entries", len(p.All()))
}

// Running a second task replaces the first: one pane, one task at a time.
func TestRunningAgainReplacesWhatWasRunning(t *testing.T) {
	m, p := build(t, sample(t.TempDir()))
	sel := m.(interface {
		Run() error
		MoveSelection(int)
	})
	if err := m.Resize(40, 6); err != nil {
		t.Fatal(err)
	}
	if err := sel.Run(); err != nil {
		t.Fatal(err)
	}
	sel.MoveSelection(1)
	if err := sel.Run(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		names := map[string]bool{}
		for _, e := range p.All() {
			names[e.Title] = true
		}
		if names["noisy"] && !names["quick"] {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the first task was not replaced by the second")
}

func TestATaskWithoutACommandIsAnError(t *testing.T) {
	_, err := module.New("tasks", map[string]any{"tasks": []any{
		map[string]any{"name": "empty"},
	}})
	if err == nil {
		t.Fatal("a task with no command was accepted")
	}
}
