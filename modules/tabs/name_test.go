package tabs_test

import (
	"fmt"
	"strings"
	"testing"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/module"
	"claudecontrol/internal/transcript"
)

// A tab is named after the directory until Claude Code has named the
// conversation, and then after the conversation.
//
// Three tabs in one project all read "DEV", "DEV 2", "DEV 3", which tells you
// nothing about which is which — and the name Claude Code gave each one is
// exactly what would. The pane title has followed that name from the start;
// the strip that replaced the pane title did not.
func TestATabTakesTheNameClaudeCodeGaveTheSession(t *testing.T) {
	b := bus.New()
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "DEV", "module": "term", "options": shell("printf HERE; cat")},
	}}, module.Context{Wake: func() {}, Bus: b})

	if strip := paint(t, m, 60, 8).row(0); !strings.Contains(strip, "DEV") {
		t.Fatalf("the strip starts as %q", strip)
	}

	ided, ok := m.(interface{ SessionID() string })
	if !ok {
		t.Fatal("a pane of tabs does not say which session it is showing")
	}
	id := ided.SessionID()
	if id == "" {
		t.Fatal("no session id for the tab on screen")
	}

	// What the application publishes as soon as a transcript names a session.
	b.PublishEvent(transcript.NameTopic, transcript.SessionName{
		SessionID: id, Name: "Analyse Wayland",
	})

	waitFor(t, "the tab to take the name", func() bool {
		return strings.Contains(paint(t, m, 60, 8).row(0), "Analyse")
	})
}

// A name for a session this pane does not hold changes nothing.
func TestANameForSomeoneElseChangesNothing(t *testing.T) {
	b := bus.New()
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "DEV", "module": "term", "options": shell("printf HERE; cat")},
	}}, module.Context{Wake: func() {}, Bus: b})

	b.PublishEvent(transcript.NameTopic, transcript.SessionName{
		SessionID: "somebody-else", Name: "Not This One",
	})
	waitFor(t, "the strip to settle", func() bool {
		return strings.Contains(paint(t, m, 60, 8).row(0), "DEV")
	})
	if strip := paint(t, m, 60, 8).row(0); strings.Contains(strip, "Not This One") {
		t.Errorf("the strip took a name that was not its own: %q", strip)
	}
}

// Two sessions naming themselves within a moment of each other. Both tabs
// have to take their name: the one that lost the race kept its directory's
// name for as long as the application ran, which is the bug this comes from —
// one tab named and two reading DEV.
func TestEveryTabTakesItsNameWhenSeveralArriveAtOnce(t *testing.T) {
	// Six of them, published without pause: on a channel that keeps only the
	// latest value, at least one name is certain to be replaced before it is
	// read. Two would let the reader win the race often enough to pass.
	const tabs = 6
	b := bus.New()
	spec := make([]any, 0, tabs)
	for i := 0; i < tabs; i++ {
		spec = append(spec, map[string]any{
			"title": "DEV", "module": "term", "options": shell("cat"),
		})
	}
	m := build(t, map[string]any{"tabs": spec}, module.Context{Wake: func() {}, Bus: b})

	// A shell publishes no resumable session, so the ids are read one tab at
	// a time from the tab on screen.
	ided := m.(interface{ SessionID() string })
	ids := make([]string, 0, tabs)
	for i := 0; i < tabs; i++ {
		m.Select(i)
		waitFor(t, "the tab to be on screen", func() bool { return ided.SessionID() != "" })
		ids = append(ids, ided.SessionID())
	}
	m.Select(0)

	want := make([]string, 0, tabs)
	for i, id := range ids {
		name := fmt.Sprintf("named-%d", i)
		want = append(want, name)
		b.PublishEvent(transcript.NameTopic, transcript.SessionName{SessionID: id, Name: name})
	}

	waitFor(t, "every tab to take its name", func() bool {
		titles := m.(interface{ Titles() []string }).Titles()
		for _, name := range want {
			found := false
			for _, got := range titles {
				if got == name {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	})
}
