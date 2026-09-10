package tabs_test

import (
	"strings"
	"testing"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/module"
	"claudecontrol/internal/transcript"
)

// tabsWithSessions builds a strip of conversations and reads back the session
// each tab holds.
func tabsWithSessions(t *testing.T, b *bus.Bus, titles ...string) (module.Module, []string) {
	t.Helper()
	spec := make([]any, 0, len(titles))
	for _, title := range titles {
		spec = append(spec, map[string]any{
			"title": title, "module": "term", "options": shell("cat"),
		})
	}
	m := build(t, map[string]any{"tabs": spec}, module.Context{Wake: func() {}, Bus: b})

	// The ids are read one tab at a time from the tab on screen.
	ided := m.(interface{ SessionID() string })
	ids := make([]string, 0, len(titles))
	for i := range titles {
		m.Select(i)
		waitFor(t, "the tab to be on screen", func() bool { return ided.SessionID() != "" })
		ids = append(ids, ided.SessionID())
	}
	m.Select(0)
	return m, ids
}

// The reading the pane title carries — the model that answered and what the
// turn carried — belongs on a tab too. The strip took the pane title's row,
// so without this, moving a conversation into a tab cost you the figures.
func TestATabSaysWhatTheTurnCarried(t *testing.T) {
	b := bus.New()
	m, ids := tabsWithSessions(t, b, "Reemo-227 clipboard limit")

	b.PublishEvent(transcript.SessionTopic, transcript.SessionMetrics{
		SessionID: ids[0],
		Metrics:   transcript.Metrics{Model: "claude-opus-5", Context: 454_000},
	})

	waitFor(t, "the reading to reach the strip", func() bool {
		row := paint(t, m, 120, 8).row(0)
		return strings.Contains(row, "opus-5 · 454k ctx")
	})

	// And a dot between the name and the model, not a gap: two spaces there
	// read as two columns of a table.
	row := paint(t, m, 120, 8).row(0)
	if !strings.Contains(row, "clipboard limit · opus-5") {
		t.Errorf("the name runs into the model: %q", row)
	}
}

// A tab with no room for the figures shows the name. Clipped, a figure is a
// number you cannot trust — and several tabs sharing a strip never have the
// room, which is where this rule earns its keep.
func TestANarrowTabKeepsItsNameAndDropsTheFigures(t *testing.T) {
	b := bus.New()
	m, ids := tabsWithSessions(t, b, "Reemo-227 clipboard limit")

	b.PublishEvent(transcript.SessionTopic, transcript.SessionMetrics{
		SessionID: ids[0],
		Metrics:   transcript.Metrics{Model: "claude-opus-5", Context: 454_000},
	})
	waitFor(t, "the reading to reach the strip", func() bool {
		return strings.Contains(paint(t, m, 120, 8).row(0), "454k ctx")
	})

	// The same strip, with only the name's worth of room.
	row := paint(t, m, 34, 8).row(0)
	if strings.Contains(row, "ctx") || strings.Contains(row, "opus") {
		t.Errorf("a figure was squeezed in where it does not fit: %q", row)
	}
	if !strings.Contains(row, "Reemo-227") {
		t.Errorf("the name went instead: %q", row)
	}
}

// Several tabs divide the strip between them, and the figures go. Two tabs on
// a wide strip keep theirs: the rule is the room, not the count.
func TestTheFiguresGoWhenTheTabsShareTheStrip(t *testing.T) {
	b := bus.New()
	m, ids := tabsWithSessions(t, b, "Stockage par groupe", "Reemo-227 clipboard limit",
		"Machines composites", "Interface visuelle", "Analyse Wayland")

	for _, id := range ids {
		b.PublishEvent(transcript.SessionTopic, transcript.SessionMetrics{
			SessionID: id,
			Metrics:   transcript.Metrics{Model: "claude-opus-5", Context: 454_000},
		})
	}
	// Five tabs across 120 columns: 24 each, and a name fills that.
	waitFor(t, "the strip to settle", func() bool {
		return strings.Contains(paint(t, m, 120, 8).row(0), "Stockage")
	})
	if row := paint(t, m, 120, 8).row(0); strings.Contains(row, "ctx") {
		t.Errorf("a figure survived a strip divided five ways: %q", row)
	}

	// The same five tabs with room for two of them to say everything.
	if row := paint(t, m, 400, 8).row(0); !strings.Contains(row, "454k ctx") {
		t.Errorf("room enough, and still no figures: %q", row)
	}
}

// A tab holding something that reports no turns says nothing beyond its name.
func TestATabWithNoTurnSaysNothingExtra(t *testing.T) {
	b := bus.New()
	m, _ := tabsWithSessions(t, b, "brain")

	row := paint(t, m, 120, 8).row(0)
	if strings.Contains(row, "·") || strings.Contains(row, "ctx") {
		t.Errorf("a tab with no turn claimed a reading: %q", row)
	}
}
