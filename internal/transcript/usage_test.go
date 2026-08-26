package transcript_test

import (
	"math"
	"testing"

	"claudecontrol/internal/transcript"
)

func TestContextIsEverythingTheRequestCarried(t *testing.T) {
	u := transcript.Usage{Input: 2, CacheRead: 22460, CacheCreation: 14519, Output: 689}
	m := transcript.Derive("claude-opus-5", u, nil)
	if want := 2 + 22460 + 14519; m.Context != want {
		t.Fatalf("Context = %d, want %d", m.Context, want)
	}
}

func TestCacheRateIsTheShareThatWasRead(t *testing.T) {
	u := transcript.Usage{Input: 10, CacheRead: 90, CacheCreation: 0}
	m := transcript.Derive("claude-opus-5", u, nil)
	if math.Abs(m.CacheRate-0.9) > 1e-9 {
		t.Fatalf("CacheRate = %g, want 0.9", m.CacheRate)
	}
}

func TestCacheRateOfAnEmptyTurnIsZeroRatherThanNaN(t *testing.T) {
	m := transcript.Derive("claude-opus-5", transcript.Usage{}, nil)
	if m.CacheRate != 0 {
		t.Fatalf("CacheRate = %g, want 0", m.CacheRate)
	}
}

// A percentage is offered only when the window is known. The published table
// of context windows is cached and the authority is the Models API, so a
// hard-coded figure would go wrong in silence — which is exactly why the spec
// keeps monetary cost out of this stage.
func TestNoPercentageForAnUnknownModel(t *testing.T) {
	m := transcript.Derive("claude-something-new", transcript.Usage{Input: 1000}, transcript.DefaultWindows())
	if m.HasWindow {
		t.Fatal("an unknown model was given a context window")
	}
	if _, ok := m.ContextPercent(); ok {
		t.Fatal("an unknown model was given a percentage")
	}
	if m.Context != 1000 {
		t.Errorf("Context = %d; the absolute figure must still be reported", m.Context)
	}
}

func TestPercentageForAConfiguredModel(t *testing.T) {
	windows := map[string]int{"tiny": 1000}
	m := transcript.Derive("tiny", transcript.Usage{Input: 250}, windows)
	pct, ok := m.ContextPercent()
	if !ok {
		t.Fatal("a configured model got no percentage")
	}
	if math.Abs(pct-25) > 1e-9 {
		t.Fatalf("ContextPercent = %g, want 25", pct)
	}
}
