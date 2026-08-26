package app

import (
	"testing"
)

func paletteApp(t *testing.T) *App {
	t.Helper()
	return settingsApp(t, "")
}

// The palette is built from the binding table, like the help panel: an action
// added without a palette entry is impossible.
func TestThePaletteListsEveryNamedBinding(t *testing.T) {
	a := paletteApp(t)
	a.togglePalette()
	if a.palette == nil {
		t.Fatal("toggling did not open the palette")
	}

	got := map[string]bool{}
	for _, e := range a.paletteEntries() {
		got[e.label] = true
	}
	for _, b := range bindings {
		if b.desc == "" || b.run == nil {
			continue
		}
		if !got[b.display] {
			t.Errorf("the palette is missing %q", b.display)
		}
	}
}

// Matching is on both the keystroke and its description, because you remember
// one or the other, not reliably both.
func TestMatchingLooksAtTheDescriptionToo(t *testing.T) {
	cases := []struct {
		query, label, desc string
		want               bool
	}{
		{"", "alt+n", "new session", true},
		{"new", "alt+n", "new session", true},
		{"alt+n", "alt+n", "new session", true},
		{"NEW", "alt+n", "new session", true},
		{"sess", "alt+n", "new session", true},
		{"zoom", "alt+n", "new session", false},
	}
	for _, c := range cases {
		if got := matches(c.query, c.label, c.desc); got != c.want {
			t.Errorf("matches(%q, %q, %q) = %v, want %v", c.query, c.label, c.desc, got, c.want)
		}
	}
}

// Typing narrows the list; backspace widens it again.
func TestTypingFiltersAndBackspaceRestores(t *testing.T) {
	a := paletteApp(t)
	a.togglePalette()
	all := len(a.paletteVisible())

	a.paletteType("zoom")
	narrowed := len(a.paletteVisible())
	if narrowed == 0 {
		t.Fatal("filtering on zoom matched nothing")
	}
	if narrowed >= all {
		t.Fatalf("filtering on zoom left %d of %d entries", narrowed, all)
	}

	for i := 0; i < 4; i++ {
		a.paletteBackspace()
	}
	if got := len(a.paletteVisible()); got != all {
		t.Fatalf("%d entries after clearing the query, want %d", got, all)
	}
}

// Choosing runs the action and closes the palette.
func TestChoosingRunsTheActionAndCloses(t *testing.T) {
	a := paletteApp(t)
	a.togglePalette()
	a.paletteType("zoom")

	before := a.zoomed
	a.paletteChoose()
	if a.palette != nil {
		t.Error("the palette stayed open after a choice")
	}
	if a.zoomed == before {
		t.Error("choosing zoom did nothing")
	}
}

// A query that matches nothing must not run whatever happened to be first.
func TestChoosingNothingRunsNothing(t *testing.T) {
	a := paletteApp(t)
	a.togglePalette()
	a.paletteType("no-such-action-anywhere")
	if got := len(a.paletteVisible()); got != 0 {
		t.Fatalf("%d entries matched a nonsense query", got)
	}
	before := a.zoomed
	a.paletteChoose()
	if a.zoomed != before {
		t.Error("choosing from an empty list ran something")
	}
	if a.palette == nil {
		t.Error("the palette closed on a choice it could not make")
	}
}

func TestMatchIsCaseInsensitiveOnBothSides(t *testing.T) {
	if !matches("ZOOM", "alt+z", "zoom / restore") {
		t.Error("an upper-case query did not match a lower-case description")
	}
}
