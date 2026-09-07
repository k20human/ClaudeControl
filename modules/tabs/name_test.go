package tabs_test

import (
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
	b.PublishState(transcript.NameTopic, transcript.SessionName{
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

	b.PublishState(transcript.NameTopic, transcript.SessionName{
		SessionID: "somebody-else", Name: "Not This One",
	})
	waitFor(t, "the strip to settle", func() bool {
		return strings.Contains(paint(t, m, 60, 8).row(0), "DEV")
	})
	if strip := paint(t, m, 60, 8).row(0); strings.Contains(strip, "Not This One") {
		t.Errorf("the strip took a name that was not its own: %q", strip)
	}
}
