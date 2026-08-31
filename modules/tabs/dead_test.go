package tabs_test

import (
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/module"
	"claudecontrol/internal/session"
)

// A pane of tabs has to answer for the tab on screen. It did not, and a
// conversation that ended inside a tab left the pane showing a dead screen
// with no banner, no key offered and nothing said — the application asks the
// pane's module whether its process is gone, and the pane never passed the
// question on.
func TestAPaneOfTabsReportsTheSessionOfTheTabOnScreen(t *testing.T) {
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "short", "module": "term", "options": shell("printf BYE; exit 3")},
		map[string]any{"title": "long", "module": "term", "options": shell("printf STAYS; cat")},
	}}, module.Context{Wake: func() {}})

	holder, ok := m.(interface{ Session() *session.Session })
	if !ok {
		t.Fatal("a pane of tabs does not answer for the session it is showing")
	}
	waitFor(t, "the first tab", func() bool {
		return strings.Contains(paint(t, m, 50, 10).text(), "BYE")
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := holder.Session(); s != nil {
			if st, code := s.Status(); st == session.Exited {
				if code != 3 {
					t.Errorf("code = %d, want 3", code)
				}
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("the pane never reported the tab's process as gone")
}

// And it answers for the tab on screen, not for whichever it likes.
func TestThePaneAnswersForTheTabOnScreen(t *testing.T) {
	m := build(t, two(), module.Context{Wake: func() {}})
	holder := m.(interface{ Session() *session.Session })
	waitFor(t, "the first tab", func() bool {
		return strings.Contains(paint(t, m, 50, 10).text(), "FIRST")
	})

	first := holder.Session()
	if first == nil {
		t.Fatal("no session for the tab on screen")
	}
	m.CycleTab(1)
	waitFor(t, "the second tab", func() bool {
		return strings.Contains(paint(t, m, 50, 10).text(), "SECOND")
	})
	if second := holder.Session(); second == first {
		t.Error("the pane still answers for the tab that is no longer on screen")
	}
}
