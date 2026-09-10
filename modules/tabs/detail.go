package tabs

import (
	"image/color"
	"strings"

	"claudecontrol/internal/transcript"
)

// What a tab says beyond its name.
//
// A pane holding one conversation has always carried this in its title: the
// model that answered, and what the turn carried. The strip replaced the pane
// title, so moving that conversation into a tab used to cost you the reading.
//
// It is shown only where the name and the reading both fit whole. A clipped
// figure is a figure you cannot trust, and a strip divided between several
// tabs has no room for any — which is the right answer there: the name is
// what you are looking for.

// tabDetailFg is the reading on a tab you are not looking at: dimmer than the
// name beside it.
var tabDetailFg = color.RGBA{R: 0x7a, G: 0x86, B: 0x99, A: 0xff}

// detailSep separates a name from its reading, and the figures from each
// other. The same separator the pane title uses, because it is the same
// reading.
const detailSep = " · "

// noteUsage records the last turn of a session and reports whether anything
// this strip shows has changed.
func (m *Module) noteUsage(turn transcript.SessionMetrics) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.usage == nil {
		m.usage = make(map[string]transcript.Metrics)
	}
	was, seen := m.usage[turn.SessionID]
	m.usage[turn.SessionID] = turn.Metrics
	if !m.holdsLocked(turn.SessionID) {
		// Another pane's conversation. Kept all the same — a tab dragged in
		// here arrives knowing what it was doing — but nothing to redraw for.
		return false
	}
	if !seen {
		return true
	}
	// Only the two figures on the strip matter: a turn that changed neither
	// changes no label, and repainting for it is work nobody asked for.
	return was.Model != turn.Metrics.Model || was.Context != turn.Metrics.Context
}

// holdsLocked reports whether one of these tabs holds a conversation.
func (m *Module) holdsLocked(id string) bool {
	if id == "" {
		return false
	}
	for _, t := range m.tabs {
		if holder, ok := t.mod.(interface{ SessionID() string }); ok &&
			holder.SessionID() == id {
			return true
		}
	}
	return false
}

// detailLocked is the reading for one tab, and "" for a tab holding something
// that reports no turns.
func (m *Module) detailLocked(t *tab) string {
	holder, ok := t.mod.(interface{ SessionID() string })
	if !ok {
		return ""
	}
	turn, ok := m.usage[holder.SessionID()]
	if !ok {
		return ""
	}
	var parts []string
	if turn.Model != "" {
		parts = append(parts, transcript.ShortModel(turn.Model))
	}
	if turn.Context > 0 {
		// Absolute counts, never a percentage: the size of a context window
		// is a published number that goes out of date, and a wrong
		// percentage is worse than none.
		parts = append(parts, transcript.HumanTokens(turn.Context)+" ctx")
	}
	return strings.Join(parts, detailSep)
}
