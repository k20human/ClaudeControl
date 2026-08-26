package app_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A stats pane is the only thing that reaches the network, and the status bar
// reprises what it found. This drives the whole application: the config is
// read, the module is built, the token is loaded from the path it was given,
// the endpoint is called, and the figure reaches the bar at the bottom.
func TestAStatsPaneShowsTheAccountBudgetInTheBar(t *testing.T) {
	const W, H = 100, 16

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"five_hour":{"utilization":37.0,"resets_at":%q},
		                 "seven_day":{"utilization":14.0,"resets_at":%q}}`,
			time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339),
			time.Now().Add(50*time.Hour).UTC().Format(time.RFC3339))
	}))
	defer srv.Close()

	dir := t.TempDir()
	creds := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(creds, []byte(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":`+
		strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)+`}}`), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	cfg := filepath.Join(dir, "stats.yaml")
	if err := os.WriteFile(cfg, []byte(fmt.Sprintf(`layout:
  split: horizontal
  ratios: [1, 1]
  children:
    - module: term
      options: { cmd: [sh, -c, "printf 'L>'; cat"] }
    - module: stats
      options:
        endpoint: %q
        credentials: %q
`, srv.URL, creds)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, snap := run(t, cfg, W, H)
	waitForRow(t, snap, paneRow0, "L>")

	// In the pane, with its bar and how long is left.
	waitForAnywhere(t, snap, "plan usage")
	waitForAnywhere(t, snap, "37%")
	waitForAnywhere(t, snap, "week")

	// And reprised in the status bar at the bottom.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if row := snap().row(H - 1); columnOf(row, "usage 5h 37% · 7d 14%") >= 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the status bar does not carry the budget: %q", snap().row(H-1))
}
