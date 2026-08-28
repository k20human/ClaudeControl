package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A supervisor in the right-hand half of a split, inside a tab — which is
// where one actually lives, and the arrangement in which its buttons did
// nothing at all.
func supervisorConfig(t *testing.T) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "sup.yaml")
	body := `layout:
  split: horizontal
  ratios: [3, 2]
  children:
    - module: term
      options: { cmd: [sh, -c, "printf 'L>'; cat"] }
    - module: tabs
      options:
        tabs:
          - title: services
            module: supervisor
            options:
              services:
                - name: alpha
                  cmd: [sh, -c, "printf ALPHA-UP; sleep 300"]
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The application hands a module a click in the module's own coordinates. A
// module that recorded its buttons in screen coordinates had buttons that
// worked only when it was drawn at the top-left corner of the screen — which
// is to say, only in its own tests.
func TestTheSupervisorsButtonsWorkFromWhereItIsDrawn(t *testing.T) {
	const W, H = 120, 20
	s, snap := run(t, supervisorConfig(t), W, H)
	waitForAnywhere(t, snap, "▸ start")
	waitForAnywhere(t, snap, "alpha")

	// Focus the pane it is in: the first click on an unfocused pane is
	// swallowed, as it is everywhere else.
	x, y := where(t, snap, "alpha")
	click(t, s, x, y)

	bx, by := where(t, snap, "▸ start")
	click(t, s, bx+2, by)

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rowWith(snap, "alpha"), "running") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the start button did nothing; the row reads %q", rowWith(snap, "alpha"))
}

// where is the first position of some text on screen.
func where(t *testing.T, snap func() *screen, want string) (x, y int) {
	t.Helper()
	g := snap()
	for row := 0; row < g.h; row++ {
		if c := columnOf(g.row(row), want); c >= 0 {
			return c, row
		}
	}
	t.Fatalf("%q is not on screen", want)
	return 0, 0
}

// rowWith is the row holding some text, or "".
func rowWith(snap func() *screen, want string) string {
	g := snap()
	for row := 0; row < g.h; row++ {
		if strings.Contains(g.row(row), want) {
			return g.row(row)
		}
	}
	return ""
}
