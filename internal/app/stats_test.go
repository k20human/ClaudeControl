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

// statsFixture serves a fixed reading and writes a config pointing at it. The
// five-hour window refills in a couple of hours and the weekly one in a couple
// of days, so the two countdowns are told apart by their unit.
func statsFixture(t *testing.T, fiveHour, sevenDay float64) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"five_hour":{"utilization":%.1f,"resets_at":%q},
		                 "seven_day":{"utilization":%.1f,"resets_at":%q}}`,
			fiveHour, time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339),
			sevenDay, time.Now().Add(50*time.Hour).UTC().Format(time.RFC3339))
	}))

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
	return srv, cfg
}

// A stats pane is the only thing that reaches the network, and the status bar
// reprises what it found. This drives the whole application: the config is
// read, the module is built, the token is loaded from the path it was given,
// the endpoint is called, and the figure reaches the bar at the bottom.
func TestAStatsPaneShowsTheAccountBudgetInTheBar(t *testing.T) {
	const W, H = 100, 16

	srv, cfg := statsFixture(t, 37, 14)
	defer srv.Close()

	_, snap := run(t, cfg, W, H)
	waitForRow(t, snap, paneRow0, "L>")

	// In the pane, with its bar and how long is left.
	waitForAnywhere(t, snap, "plan usage")
	waitForAnywhere(t, snap, "37%")
	waitForAnywhere(t, snap, "week")

	// And reprised in the status bar at the bottom. At a hundred columns there
	// is room for the shares and the first countdown, but not the second.
	deadline := time.Now().Add(5 * time.Second)
	var row string
	for time.Now().Before(deadline) {
		if row = snap().row(H - 1); columnOf(row, "usage 5h 37% ↻ 1h") >= 0 {
			if columnOf(row, "· 7d 14%") < 0 {
				t.Errorf("the weekly share is missing: %q", row)
			}
			// The buttons that matter must survive the figure.
			for _, want := range []string{"new", "close", "list", "help"} {
				if columnOf(row, want) < 0 {
					t.Errorf("the budget pushed %q off the bar: %q", want, row)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the status bar does not carry the budget: %q", row)
}

// The bar takes the widest shape that still leaves the buttons that matter,
// so the figure never costs you the way back to the rest of the interface.
func TestTheBudgetInTheBarNarrowsRatherThanCrowding(t *testing.T) {
	srv, cfg := statsFixture(t, 42, 15)
	defer srv.Close()

	for _, c := range []struct {
		w    int
		want []string
		gone []string
	}{
		{120, []string{"usage 5h 42% ↻", "· 7d 15% ↻"}, nil},
		{100, []string{"usage 5h 42% ↻", "· 7d 15%"}, nil},
		{86, []string{"usage 5h 42%", "· 7d 15%"}, []string{"↻"}},
		{82, []string{"usage 5h 42%"}, []string{"7d 15%"}},
	} {
		t.Run(fmt.Sprint(c.w), func(t *testing.T) {
			_, snap := run(t, cfg, c.w, 14)
			waitForAnywhere(t, snap, "42%")

			deadline := time.Now().Add(4 * time.Second)
			var row string
			for time.Now().Before(deadline) {
				row = snap().row(13)
				if columnOf(row, c.want[0]) >= 0 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			for _, want := range c.want {
				if columnOf(row, want) < 0 {
					t.Errorf("the bar lacks %q: %q", want, row)
				}
			}
			for _, gone := range c.gone {
				if columnOf(row, gone) >= 0 {
					t.Errorf("the bar kept %q it had no room for: %q", gone, row)
				}
			}
			// Whatever the width, these stay.
			for _, want := range []string{"new", "close", "list", "help", "quit"} {
				if columnOf(row, want) < 0 {
					t.Errorf("the budget pushed %q off the bar: %q", want, row)
				}
			}
		})
	}
}
