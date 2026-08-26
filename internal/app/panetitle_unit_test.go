package app

import (
	"testing"

	"claudecontrol/internal/layout"
)

func TestContentRectGivesUpTheTitleRow(t *testing.T) {
	got := contentRect(layout.Rect{X: 4, Y: 2, W: 20, H: 6})
	want := layout.Rect{X: 4, Y: 3, W: 20, H: 5}
	if got != want {
		t.Errorf("contentRect = %v, want %v", got, want)
	}
	// A pane too short for both keeps its origin and has no content, rather
	// than reporting a negative height that callers would have to guard.
	if got := contentRect(layout.Rect{X: 1, Y: 1, W: 8, H: 1}); got.H != 0 {
		t.Errorf("a one-row pane leaves %d content rows, want 0", got.H)
	}
}

func TestHumanTokensStaysShortWithoutLying(t *testing.T) {
	for _, c := range []struct {
		in   int
		want string
	}{
		{0, "0"},
		{812, "812"},
		{1_200, "1.2k"},
		{34_000, "34k"},
		{999_999, "999k"},
		{1_250_000, "1.2M"},
	} {
		if got := humanTokens(c.in); got != c.want {
			t.Errorf("humanTokens(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruncateMarksWhatItCut(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
	}{
		{"claude", 10, "claude"},
		{"claude", 6, "claude"},
		{"claude", 5, "clau…"},
		{"claude", 1, "…"},
		{"claude", 0, ""},
	} {
		if got := truncate(c.in, c.w); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}

func TestShortModelDropsTheVendorPrefix(t *testing.T) {
	if got := shortModel("claude-opus-5"); got != "opus-5" {
		t.Errorf("shortModel = %q, want %q", got, "opus-5")
	}
	if got := shortModel("something-else"); got != "something-else" {
		t.Errorf("shortModel rewrote an unrelated name to %q", got)
	}
}
