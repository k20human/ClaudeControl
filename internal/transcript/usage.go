package transcript

import (
	"fmt"
	"strings"
)

// SessionTopic is where per-session metrics are published.
//
// The topic and its payload live here rather than beside the publisher: a
// module that wants to display them cannot import the application, which
// already imports modules.
const SessionTopic = "session.usage"

// SessionMetrics ties a turn's metrics to the session that produced them.
type SessionMetrics struct {
	SessionID string
	Metrics   Metrics
}

// NameTopic is where the name Claude Code gave a session is published.
const NameTopic = "session.name"

// SessionName ties that name to its session.
type SessionName struct {
	SessionID string
	Name      string
}

// ShortModel drops the vendor prefix, which is the same on every line and
// tells you nothing.
func ShortModel(model string) string {
	return strings.TrimPrefix(model, "claude-")
}

// HumanTokens keeps a token count to a few characters without lying about its
// order of magnitude.
func HumanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// Metrics are what one assistant turn says about the session.
type Metrics struct {
	Context   int     // tokens the request carried
	CacheRate float64 // share of that context which came from cache
	Thinking  int     // thinking tokens of the turn
	Model     string
	Window    int  // context window, when known
	HasWindow bool // whether Window means anything
}

// DefaultWindows are context windows as published on 2026-08-26.
//
// They are a convenience, not an authority. The published table is itself
// cached, and the real source is the Models API field max_input_tokens — so
// anything absent from this map gets no percentage rather than a wrong one.
// The same reasoning keeps monetary cost out of this stage entirely.
//
// When one of these drifts, the correction is to remove the entry, never to
// invent a replacement.
func DefaultWindows() map[string]int {
	const m = 1_000_000
	return map[string]int{
		"claude-opus-5":     m,
		"claude-fable-5":    m,
		"claude-sonnet-5":   m,
		"claude-opus-4-8":   m,
		"claude-opus-4-7":   m,
		"claude-opus-4-6":   m,
		"claude-sonnet-4-6": m,
		"claude-haiku-4-5":  200_000,
	}
}

// Derive reduces a turn to its metrics. A nil windows map means no percentage
// is offered for any model.
func Derive(model string, u Usage, windows map[string]int) Metrics {
	m := Metrics{
		Context:  u.Input + u.CacheRead + u.CacheCreation,
		Thinking: u.Thinking,
		Model:    model,
	}
	if m.Context > 0 {
		m.CacheRate = float64(u.CacheRead) / float64(m.Context)
	}
	if w, ok := windows[model]; ok && w > 0 {
		m.Window, m.HasWindow = w, true
	}
	return m
}

// ContextPercent returns how full the context is, and whether that can be said
// at all.
func (m Metrics) ContextPercent() (float64, bool) {
	if !m.HasWindow || m.Window <= 0 {
		return 0, false
	}
	return float64(m.Context) * 100 / float64(m.Window), true
}
