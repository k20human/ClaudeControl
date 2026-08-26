package settings_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claudecontrol/internal/settings"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const configured = `# My control centre
layout:
  split: horizontal
  ratios: [2, 1]
  children:
    # the session I actually work in
    - module: claude
      options:
        dir: ~/DEV
    - module: hologram
      options:
        style: sphere
        speed: 0.18   # validated by eye
        trail: 0.90
`

// The whole point of going through the syntax tree: a slider must not cost
// somebody their comments.
func TestWritingASettingKeepsEveryComment(t *testing.T) {
	p := write(t, configured)
	if err := settings.WritePaneOptions(p, 1, map[string]any{"speed": 0.42}); err != nil {
		t.Fatalf("WritePaneOptions: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)

	for _, comment := range []string{
		"# My control centre",
		"# the session I actually work in",
		"# validated by eye",
	} {
		if !strings.Contains(out, comment) {
			t.Errorf("comment %q was lost:\n%s", comment, out)
		}
	}
	if !strings.Contains(out, "0.42") {
		t.Errorf("the new value is missing:\n%s", out)
	}
	if strings.Contains(out, "0.18") {
		t.Errorf("the old value survived:\n%s", out)
	}
	if !strings.Contains(out, "trail: 0.90") {
		t.Errorf("a sibling setting was disturbed:\n%s", out)
	}
}

// Panes are addressed in document order, the same order config.Build numbers
// them, so the ordinal a caller has is the one that lands.
func TestWritingAddressesThePaneByDocumentOrder(t *testing.T) {
	p := write(t, configured)
	if err := settings.WritePaneOptions(p, 0, map[string]any{"dir": "/tmp/elsewhere"}); err != nil {
		t.Fatalf("WritePaneOptions: %v", err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "/tmp/elsewhere") {
		t.Fatalf("the first pane was not the one written:\n%s", got)
	}
}

// A key that is not in the file yet has to be added rather than dropped.
func TestWritingAddsAKeyThatWasNotThere(t *testing.T) {
	p := write(t, configured)
	if err := settings.WritePaneOptions(p, 1, map[string]any{"breath": 4.5}); err != nil {
		t.Fatalf("WritePaneOptions: %v", err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "breath") {
		t.Fatalf("the new key was not added:\n%s", got)
	}
}

// A file nobody has written yet is normal, and writing must create it.
func TestWritingCreatesAMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "config.yaml")
	if err := settings.ReplaceLayout(p, map[string]any{"module": "claude"}); err != nil {
		t.Fatalf("ReplaceLayout: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("the file was not created: %v", err)
	}
	if !strings.Contains(string(got), "claude") {
		t.Fatalf("the layout was not written:\n%s", got)
	}
}

// Refusing to write over a file we cannot parse is the point: overwriting it
// would replace somebody's work with defaults, and the mistake would be
// invisible.
func TestWritingRefusesAnUnparseableFile(t *testing.T) {
	p := write(t, "layout: [this is not a mapping\n")
	err := settings.WritePaneOptions(p, 0, map[string]any{"speed": 1})
	if err == nil {
		t.Fatal("an unparseable file was overwritten")
	}
	if !errors.Is(err, settings.ErrUnreadable) {
		t.Fatalf("error = %v, want it to wrap ErrUnreadable", err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "this is not a mapping") {
		t.Fatal("the unparseable file was modified anyway")
	}
}

// Replacing the layout keeps the rest of the document, comments included.
func TestReplacingTheLayoutKeepsTheRestOfTheDocument(t *testing.T) {
	p := write(t, "# top of file\n"+configured+"\n# trailing note\nsomething_else: 3\n")
	if err := settings.ReplaceLayout(p, map[string]any{"module": "sessions"}); err != nil {
		t.Fatalf("ReplaceLayout: %v", err)
	}
	got, _ := os.ReadFile(p)
	out := string(got)
	if !strings.Contains(out, "# top of file") {
		t.Errorf("the leading comment was lost:\n%s", out)
	}
	if !strings.Contains(out, "something_else: 3") {
		t.Errorf("an unrelated key was lost:\n%s", out)
	}
	if !strings.Contains(out, "sessions") {
		t.Errorf("the new layout is missing:\n%s", out)
	}
}
