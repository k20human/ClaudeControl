package supervisor_test

import (
	"testing"

	"claudecontrol/modules/supervisor"
)

// A service's log is exactly where a search is wanted, and it was the one
// place the pane search refused to open.
func TestAServiceLogIsSearchable(t *testing.T) {
	m := build(t, map[string]any{"services": []any{
		map[string]any{"name": "chatty", "cmd": []any{
			"sh", "-c", "i=1; while [ $i -le 90 ]; do printf 'line-%03d\\n' $i; i=$((i+1)); done; sleep 300"}},
	}})
	// The interface the application looks for, asserted here so that renaming
	// a method breaks this test rather than the search.
	var search interface {
		CanFind() bool
		Find(string) int
		FindStatus() (string, int, int)
		FindClear()
	} = m

	if search.CanFind() {
		t.Error("a service that is down offers a search that can only answer none")
	}

	m.StartPicked()
	waitFor(t, "the service", func() bool { return stateOf(m, "chatty").State == supervisor.Running })

	// Asking must not move the pane: the menu asks on every right-click.
	if !search.CanFind() {
		t.Error("CanFind says no while a service is running")
	}
	if m.Showing() != -1 {
		t.Errorf("asking whether a search is possible opened the log (Showing = %d)", m.Showing())
	}

	waitFor(t, "its output", func() bool { return search.Find("line-012") > 0 })

	// Searching from the list opens the highlighted service's log: a search you
	// have to set up before you can run is a search you will not run.
	if m.Showing() != 0 {
		t.Errorf("Showing = %d; the search should have opened the log", m.Showing())
	}
	if q, _, total := search.FindStatus(); q != "line-012" || total != 1 {
		t.Errorf("FindStatus = %q %d, want the query and one match", q, total)
	}

	search.FindClear()
	if q, _, total := search.FindStatus(); q != "" || total != 0 {
		t.Errorf("FindStatus after clearing = %q %d", q, total)
	}
}
