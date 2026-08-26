package services_test

import (
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/module"
	"claudecontrol/internal/probe"
	"claudecontrol/modules/services"
)

// The row is what an operator reads at a glance: the name, the verdict, and
// why when there is a why.
func TestRowShowsTheVerdictAndItsReason(t *testing.T) {
	cases := []struct {
		result probe.Result
		want   []string
	}{
		{probe.Result{Name: "postgres", Health: probe.HealthUp}, []string{"postgres", "up"}},
		{probe.Result{Name: "relay", Health: probe.HealthDown, Detail: "500 Server Error"}, []string{"relay", "down", "500"}},
		{probe.Result{Name: "redis", Health: probe.HealthUnknown}, []string{"redis", "unknown"}},
	}
	for _, c := range cases {
		got := services.Row(c.result, 60)
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("Row(%s) = %q, missing %q", c.result.Name, got, want)
			}
		}
	}
}

func TestRowIsClippedToTheWidth(t *testing.T) {
	r := probe.Result{
		Name:   strings.Repeat("service-", 12),
		Health: probe.HealthDown,
		Detail: strings.Repeat("because-", 20),
	}
	for _, w := range []int{12, 30, 70} {
		if got := services.Row(r, w); len([]rune(got)) > w {
			t.Errorf("Row at width %d returned %d runes", w, len([]rune(got)))
		}
	}
}

// A configured check has to become a probe of the right kind.
func TestEachKindOfCheckIsAccepted(t *testing.T) {
	cfg := map[string]any{
		"interval": 1,
		"checks": []any{
			map[string]any{"name": "by-command", "cmd": []any{"true"}},
			map[string]any{"name": "by-http", "http": "http://127.0.0.1:1/health"},
			map[string]any{"name": "by-port", "tcp": "127.0.0.1:1"},
		},
	}
	m, err := module.New("services", cfg)
	if err != nil {
		t.Fatalf("module.New: %v", err)
	}
	b := bus.New()
	defer b.Close()
	if err := m.Init(module.Context{PaneID: 1, Bus: b, Wake: func() {}}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer m.Close()

	lister, ok := m.(interface{ Results() []probe.Result })
	if !ok {
		t.Fatal("the services module does not expose its results")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(lister.Results()) == 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the module holds %d results, want 3", len(lister.Results()))
}

// A check with no name, or with none of the three kinds, is a configuration
// mistake and has to be reported rather than skipped.
func TestAnIncompleteCheckIsAnError(t *testing.T) {
	for _, checks := range []any{
		[]any{map[string]any{"cmd": []any{"true"}}},
		[]any{map[string]any{"name": "nothing"}},
	} {
		if _, err := module.New("services", map[string]any{"checks": checks}); err == nil {
			t.Errorf("an incomplete check was accepted: %v", checks)
		}
	}
}
