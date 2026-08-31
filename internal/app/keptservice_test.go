package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// A supervisor keeping its services, inside a pane of tabs — the arrangement
// it actually lives in.
func keptConfig(t *testing.T, marker string) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "kept.yaml")
	body := `layout:
  module: tabs
  options:
    tabs:
      - title: services
        module: supervisor
        options:
          keep_running: true
          services:
            - name: keeper
              cmd: [sh, -c, "printf '` + marker + `\n'; sleep 300"]
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Closing the application leaves a kept service running, and opening it again
// has to find it. Showing "stopped" would be worse than a wrong word: the next
// press of start launches a second copy, and two servers fight over one port.
func TestAKeptServiceIsFoundAgainByAFreshRun(t *testing.T) {
	const W, H = 100, 16
	state := t.TempDir()
	cfg := keptConfig(t, "KEEPER-UP")

	first, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, []string{"XDG_STATE_HOME=" + state})
	waitForAnywhere(t, snap, "keeper")
	click(t, first, 10, 5)

	// Start it: the row is the first, and s acts on the row.
	first.SendText("s")
	waitForAnywhere(t, snap, "running")

	// Quit, leaving the service alone.
	first.SendText("\x1bq")
	waitForAnywhere(t, snap, "save this layout")
	first.SendText("\r")
	time.Sleep(900 * time.Millisecond)

	second, snap2 := runWithEnv(t, cfg, W, H, vt.Callbacks{}, []string{"XDG_STATE_HOME=" + state})
	t.Cleanup(func() {
		// Whatever this proves, nothing of its own outlives it.
		second.SendText("\x1bq")
		time.Sleep(200 * time.Millisecond)
	})

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(dump(snap2()), "running") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("the second run does not see the service it left running:\n%s", dump(snap2()))
}
