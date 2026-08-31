package tabs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// named makes a directory whose last element is short and distinctive, so a
// tab can say where it is in one word. A temporary directory's full path is
// long enough to wrap in a pane, and a test that failed on the wrapping would
// be a test about nothing.
func named(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// sayWhere is a tab that prints the last element of the directory it is in.
func sayWhere(dir string) map[string]any {
	opts := map[string]any{"cmd": []any{"sh", "-c", "basename $PWD; cat"}}
	if dir != "" {
		opts["dir"] = dir
	}
	return opts
}

// A tab opened from a pane opens where that pane is working. It used to open
// wherever the application itself was launched from, which is rarely where you
// are.
func TestANewTabOpensWhereTheTabOnScreenIsWorking(t *testing.T) {
	here := named(t, "here")
	m := movable(t, map[string]any{"tabs": []any{
		map[string]any{"title": "first", "module": "term", "options": sayWhere(here)},
	}})
	waitFor(t, "the first tab", func() bool {
		return strings.Contains(paint(t, m, 60, 10).text(), "here")
	})

	adder := m.(interface {
		Add(string, map[string]any) error
	})
	if err := adder.Add("term", sayWhere("")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	waitFor(t, "the new tab to open beside it", func() bool {
		return strings.Contains(paint(t, m, 60, 10).text(), "here")
	})
}

// A pane can say where its tabs belong, which is what makes the inheritance
// something you can change rather than something you are given.
func TestAPaneCanSayWhereItsTabsOpen(t *testing.T) {
	home := named(t, "home")
	elsewhere := named(t, "elsewhere")
	m := movable(t, map[string]any{
		"dir": home,
		"tabs": []any{
			map[string]any{"title": "first", "module": "term", "options": sayWhere(elsewhere)},
		},
	})
	waitFor(t, "the first tab", func() bool {
		return strings.Contains(paint(t, m, 60, 10).text(), "elsewhere")
	})

	adder := m.(interface {
		Add(string, map[string]any) error
	})
	if err := adder.Add("term", sayWhere("")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	waitFor(t, "the new tab to open where the pane says", func() bool {
		return strings.Contains(paint(t, m, 60, 10).text(), "home")
	})
}

// What the caller asks for is what it gets. Neither the pane nor the tab on
// screen overrides a directory somebody named.
func TestADirectoryAskedForIsHonoured(t *testing.T) {
	asked := named(t, "asked")
	m := movable(t, map[string]any{
		"dir": named(t, "paneside"),
		"tabs": []any{
			map[string]any{"title": "first", "module": "term", "options": sayWhere(named(t, "tabside"))},
		},
	})
	adder := m.(interface {
		Add(string, map[string]any) error
	})
	if err := adder.Add("term", sayWhere(asked)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	waitFor(t, "the tab to open where it was told", func() bool {
		return strings.Contains(paint(t, m, 60, 10).text(), "asked")
	})
}
