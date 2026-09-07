package tabs_test

import (
	"strings"
	"testing"

	"claudecontrol/internal/module"
)

// Tabs fill the strip, the way a terminal's do. Compact labels left most of
// the row as empty background, which reads as a row of small buttons rather
// than as tabs — and left a click on a tab needing more aim than it should.
func TestTabsFillTheStrip(t *testing.T) {
	const w = 60
	m := build(t, two(), module.Context{Wake: func() {}})
	if err := m.Resize(w, 8); err != nil {
		t.Fatal(err)
	}
	paint(t, m, w, 8)

	// Measured rather than read off the row: a tab padded out to its share
	// looks the same as a gap, and the difference is the whole point.
	widths, ok := m.(interface {
		TabWidths() []int
		PlusColumn() int
	})
	if !ok {
		t.Fatal("a pane of tabs does not say how it divided its strip")
	}
	total := 0
	for _, n := range widths.TabWidths() {
		total += n
	}
	if plus := widths.PlusColumn(); total != plus {
		t.Errorf("the tabs cover %d columns of the %d before the +", total, plus)
	}
}

// And each tab gets its share, so two tabs split the strip in half rather than
// hugging the left edge.
func TestTabsShareTheWidthEvenly(t *testing.T) {
	const w = 60
	m := build(t, two(), module.Context{Wake: func() {}})
	if err := m.Resize(w, 8); err != nil {
		t.Fatal(err)
	}
	paint(t, m, w, 8)

	widths, ok := m.(interface{ TabWidths() []int })
	if !ok {
		t.Fatal("a pane of tabs does not say how wide its tabs are")
	}
	got := widths.TabWidths()
	if len(got) != 2 {
		t.Fatalf("%d widths for two tabs", len(got))
	}
	if got[0] < 20 || got[1] < 20 {
		t.Errorf("widths %v; two tabs in sixty columns should have about half each", got)
	}
	if diff := got[0] - got[1]; diff > 1 || diff < -1 {
		t.Errorf("widths %v are not an even split", got)
	}
}

// A strip too narrow for everything still says how much is missing rather
// than shrinking every tab to nothing.
func TestANarrowStripSaysWhatIsHidden(t *testing.T) {
	m := build(t, map[string]any{"tabs": []any{
		map[string]any{"title": "one", "module": "term", "options": shell("cat")},
		map[string]any{"title": "two", "module": "term", "options": shell("cat")},
		map[string]any{"title": "three", "module": "term", "options": shell("cat")},
		map[string]any{"title": "four", "module": "term", "options": shell("cat")},
		map[string]any{"title": "five", "module": "term", "options": shell("cat")},
	}}, module.Context{Wake: func() {}})
	if err := m.Resize(24, 8); err != nil {
		t.Fatal(err)
	}
	strip := paint(t, m, 24, 8).row(0)
	if !strings.Contains(strip, "…") {
		t.Errorf("a strip too narrow for five tabs says nothing about it: %q", strip)
	}
}
