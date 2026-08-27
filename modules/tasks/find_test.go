package tasks_test

import (
	"testing"
	"time"
)

// A task prints a great deal and the line that matters is rarely the last one,
// so the pane search has to reach it — and must not open over a task that has
// never run.
func TestATasksOutputIsSearchable(t *testing.T) {
	m, _ := build(t, map[string]any{"tasks": []any{
		map[string]any{"name": "chatty", "cmd": []any{
			"sh", "-c", "i=1; while [ $i -le 90 ]; do printf 'line-%03d\\n' $i; i=$((i+1)); done; sleep 5"}},
	}})
	if err := m.Resize(40, 8); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	search := m.(interface {
		CanFind() bool
		Find(string) int
		FindStatus() (string, int, int)
	})
	if search.CanFind() {
		t.Error("a task that has never run offers a search that can only answer none")
	}

	if err := m.(interface{ Run() error }).Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if search.Find("line-012") > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if n := search.Find("line-012"); n != 1 {
		t.Fatalf("Find found %d matches for a line printed once", n)
	}
	if !search.CanFind() {
		t.Error("CanFind says no while a task is running")
	}
	if q, _, total := search.FindStatus(); q != "line-012" || total != 1 {
		t.Errorf("FindStatus = %q %d, want the query and one match", q, total)
	}
}
