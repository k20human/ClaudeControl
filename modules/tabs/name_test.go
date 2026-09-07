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

// A name you type is yours. Claude Code renames a conversation as it goes, so
// a name of your own that anything published could overwrite would be a name
// that lasted until the next turn.
func TestANameYouTypeSurvivesTheNameClaudeCodeGives(t *testing.T) {
	b := bus.New()
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "DEV", "module": "term", "options": shell("cat")},
	}}, module.Context{Wake: func() {}, Bus: b})

	renamer := m.(interface {
		Rename(int, string) bool
		TitleAt(int) string
		SessionAt(int) string
		GivenNames() map[string]string
	})
	if !renamer.Rename(0, "facturation") {
		t.Fatal("the tab refused a name")
	}
	if got := renamer.TitleAt(0); got != "facturation" {
		t.Fatalf("the tab is called %q", got)
	}

	id := renamer.SessionAt(0)
	if id == "" {
		t.Fatal("no session behind the tab, so nothing to publish about")
	}
	b.PublishEvent(transcript.NameTopic, transcript.SessionName{
		SessionID: id, Name: "Analyse Wayland",
	})
	waitFor(t, "the strip to settle", func() bool {
		return strings.Contains(paint(t, m, 60, 8).row(0), "facturation")
	})
	if got := renamer.TitleAt(0); got != "facturation" {
		t.Errorf("a published name took the tab over: %q", got)
	}

	// And it is reported against its conversation, which is what the
	// application writes down.
	if got := renamer.GivenNames()[id]; got != "facturation" {
		t.Errorf("the name is remembered as %q against %q", got, id)
	}
}

// An empty name hands the tab back. The automatic name applies again, which is
// how you undo a name you no longer want without inventing one.
func TestAnEmptyNameHandsTheTabBack(t *testing.T) {
	b := bus.New()
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "DEV", "module": "term", "options": shell("cat")},
	}}, module.Context{Wake: func() {}, Bus: b})

	renamer := m.(interface {
		Rename(int, string) bool
		TitleAt(int) string
		SessionAt(int) string
		GivenNames() map[string]string
	})
	renamer.Rename(0, "facturation")
	renamer.Rename(0, "   ")

	if len(renamer.GivenNames()) != 0 {
		t.Errorf("a name given back is still remembered: %v", renamer.GivenNames())
	}
	id := renamer.SessionAt(0)
	b.PublishEvent(transcript.NameTopic, transcript.SessionName{
		SessionID: id, Name: "Analyse Wayland",
	})
	waitFor(t, "the published name", func() bool {
		return renamer.TitleAt(0) == "Analyse Wayland"
	})
}

// A renamed tab dragged into another pane keeps its name, and keeps it for
// good: the flag saying the name is yours travels with the tab.
func TestANameTravelsWithTheTab(t *testing.T) {
	b := bus.New()
	from := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "DEV", "module": "term", "options": shell("cat")},
	}}, module.Context{Wake: func() {}, Bus: b})
	to := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "other", "module": "term", "options": shell("cat")},
	}}, module.Context{Wake: func() {}, Bus: b})

	src := from.(interface {
		Rename(int, string) bool
		Detach(int) (module.Module, module.Held, bool)
	})
	src.Rename(0, "facturation")
	mod, held, ok := src.Detach(0)
	if !ok {
		t.Fatal("the tab would not detach")
	}
	dst := to.(interface {
		Adopt(module.Module, module.Held) error
		TitleAt(int) string
		SessionAt(int) string
	})
	if err := dst.Adopt(mod, held); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if got := dst.TitleAt(1); got != "facturation" {
		t.Fatalf("the tab arrived as %q", got)
	}

	id := dst.SessionAt(1)
	b.PublishEvent(transcript.NameTopic, transcript.SessionName{
		SessionID: id, Name: "Analyse Wayland",
	})
	waitFor(t, "the strip to settle", func() bool {
		return strings.Contains(paint(t, to, 60, 8).row(0), "facturation")
	})
	if got := dst.TitleAt(1); got != "facturation" {
		t.Errorf("the move cost the tab its name: %q", got)
	}
}
