package sessions_test

import (
	"strings"
	"testing"

	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
	"claudecontrol/modules/sessions"
)

// The row is the whole point of the module: one line that says what a session
// is doing and whether it needs you.
func TestRowShowsStateAndAttachment(t *testing.T) {
	cases := []struct {
		entry *pool.Entry
		want  []string
	}{
		{
			&pool.Entry{Title: "portal", State: pool.StateWorking, Attached: true},
			[]string{"portal", "working"},
		},
		{
			&pool.Entry{Title: "api", State: pool.StateWaiting, Attached: true},
			[]string{"api", "WAITING FOR YOU"},
		},
		{
			&pool.Entry{Title: "ndb", State: pool.StateWorking, Attached: false},
			[]string{"ndb", "detached", "working"},
		},
		{
			&pool.Entry{Title: "relay", State: pool.StateExited},
			[]string{"relay", "exited"},
		},
	}
	for _, c := range cases {
		got := sessions.Row(c.entry, 60)
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("Row(%s) = %q, missing %q", c.entry.Title, got, want)
			}
		}
	}
}

// A row must never overflow its pane, whatever the title.
func TestRowIsClippedToTheWidth(t *testing.T) {
	e := &pool.Entry{Title: strings.Repeat("long-", 30), State: pool.StateIdle}
	for _, w := range []int{10, 24, 60} {
		if got := sessions.Row(e, w); len([]rune(got)) > w {
			t.Errorf("Row at width %d returned %d runes", w, len([]rune(got)))
		}
	}
}

func TestSelectionFollowsTheEntries(t *testing.T) {
	m, err := sessions.New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sm, ok := m.(interface {
		SetEntries([]*pool.Entry)
		Selected() (session.ID, bool)
		MoveSelection(int)
	})
	if !ok {
		t.Fatal("the sessions module does not expose its selection")
	}

	if _, ok := sm.Selected(); ok {
		t.Error("an empty list reported a selection")
	}
	sm.SetEntries([]*pool.Entry{
		{Session: &session.Session{ID: "a"}, Title: "a"},
		{Session: &session.Session{ID: "b"}, Title: "b"},
	})
	if id, ok := sm.Selected(); !ok || id != "a" {
		t.Fatalf("Selected() = %q, %v; want a, true", id, ok)
	}
	sm.MoveSelection(1)
	if id, _ := sm.Selected(); id != "b" {
		t.Errorf("Selected() after moving down = %q, want b", id)
	}
	sm.MoveSelection(5)
	if id, _ := sm.Selected(); id != "b" {
		t.Errorf("Selected() past the end = %q, want it to stop at b", id)
	}
}
