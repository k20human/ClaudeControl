package stats_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
	"claudecontrol/internal/transcript"
	_ "claudecontrol/modules/stats"
)

const payload = `{
  "five_hour": {"utilization": 37.0, "resets_at": "%FIVE%"},
  "seven_day": {"utilization": 92.0, "resets_at": "%SEVEN%"},
  "limits": [{"kind": "weekly_scoped", "percent": 3, "resets_at": null,
              "scope": {"model": {"display_name": "Fable"}}}]
}`

// grid is a screen that remembers what was painted, so a test can read the
// panel back as text.
type grid struct {
	w, h  int
	cells []uv.Cell
}

func newGrid(w, h int) *grid {
	g := &grid{w: w, h: h, cells: make([]uv.Cell, w*h)}
	for i := range g.cells {
		g.cells[i] = uv.EmptyCell
	}
	return g
}

func (g *grid) Bounds() uv.Rectangle { return uv.Rect(0, 0, g.w, g.h) }
func (g *grid) CellAt(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return nil
	}
	return &g.cells[y*g.w+x]
}
func (g *grid) SetCell(x, y int, c *uv.Cell) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h || c == nil {
		return
	}
	g.cells[y*g.w+x] = *c
}
func (g *grid) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }

func (g *grid) text() string {
	var b strings.Builder
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			b.WriteString(g.cells[y*g.w+x].Content)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func credentials(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".credentials.json")
	body := `{"claudeAiOauth":{"accessToken":"tok","expiresAt":` +
		strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10) + `}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	return path
}

func build(t *testing.T, cfg map[string]any, ctx module.Context) module.Module {
	t.Helper()
	m, err := module.New("stats", cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := m.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func paint(t *testing.T, m module.Module, w, h int) string {
	t.Helper()
	g := newGrid(w, h)
	m.Draw(g, uv.Rect(0, 0, w, h))
	return g.text()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestTheAccountBudgetsAppearWithTheirBars(t *testing.T) {
	body := strings.NewReplacer(
		"%FIVE%", time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339),
		"%SEVEN%", time.Now().Add(50*time.Hour).UTC().Format(time.RFC3339),
	).Replace(payload)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	m := build(t, map[string]any{
		"endpoint": srv.URL, "credentials": credentials(t),
	}, module.Context{})

	var out string
	waitFor(t, "the budgets", func() bool {
		out = paint(t, m, 60, 12)
		return strings.Contains(out, "37%")
	})

	for _, want := range []string{"5h", "92%", "week", "Fable", "█", "░"} {
		if !strings.Contains(out, want) {
			t.Errorf("the panel does not mention %q:\n%s", want, out)
		}
	}
	// Time left, not a clock time: a clock time means nothing without another
	// clock to compare it against. The five-hour budget refills in under a
	// day, the weekly one in two.
	if !strings.Contains(out, "refills in 1h") || !strings.Contains(out, "refills in 2d") {
		t.Errorf("the panel does not say how long is left:\n%s", out)
	}

	// Narrow, the same facts abbreviate rather than disappear.
	narrow := paint(t, m, 34, 12)
	for _, want := range []string{"5h", "37%", "↻ 1h", "week"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("a narrow panel drops %q:\n%s", want, narrow)
		}
	}
	if strings.Contains(narrow, "refills in") {
		t.Errorf("a narrow panel kept the long wording:\n%s", narrow)
	}
}

// A failure has to be legible as a failure. Showing nothing, or showing zero,
// would read as "nothing used".
func TestAFailedReadingSaysWhy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := build(t, map[string]any{
		"endpoint": srv.URL, "credentials": credentials(t),
	}, module.Context{})

	var out string
	waitFor(t, "the failure", func() bool {
		out = paint(t, m, 60, 12)
		return strings.Contains(out, "unavailable")
	})
	if !strings.Contains(out, "401") {
		t.Errorf("the panel does not say why:\n%s", out)
	}
	if strings.Contains(out, "0%") {
		t.Errorf("a failed reading was drawn as a used share:\n%s", out)
	}
}

// Nothing should be sent anywhere unless the account panel was asked for.
func TestAccountFalseAsksNobody(t *testing.T) {
	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		asked = true
	}))
	defer srv.Close()

	m := build(t, map[string]any{
		"account": false, "endpoint": srv.URL, "credentials": credentials(t),
	}, module.Context{})

	time.Sleep(300 * time.Millisecond)
	if asked {
		t.Error("the endpoint was called even though account is false")
	}
	if out := paint(t, m, 60, 12); !strings.Contains(out, "not asked") {
		t.Errorf("the panel does not say the account was not asked:\n%s", out)
	}
}

func TestASessionShowsItsLastTurn(t *testing.T) {
	b := bus.New()
	defer b.Close()
	p := pool.New(b)

	s, err := session.Start(session.Spec{
		ID: "sess-1", Argv: []string{"sh", "-c", "sleep 30"}, Width: 20, Height: 4,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close()
	p.Add(s, "worker", "")

	woke := make(chan struct{}, 8)
	m := build(t, map[string]any{"account": false}, module.Context{
		Pool: p, Bus: b, Wake: func() {
			select {
			case woke <- struct{}{}:
			default:
			}
		},
	})

	// Before any turn, the session is listed but reports nothing rather than
	// zero.
	out := paint(t, m, 80, 12)
	if !strings.Contains(out, "worker") || !strings.Contains(out, "no turn yet") {
		t.Fatalf("a session with no turn is not listed plainly:\n%s", out)
	}

	b.PublishState(transcript.SessionTopic, transcript.SessionMetrics{
		SessionID: "sess-1",
		Metrics: transcript.Metrics{
			Model: "claude-opus-5", Context: 34_000, CacheRate: 0.41, Thinking: 1_200,
		},
	})
	<-woke

	waitFor(t, "the turn", func() bool {
		out = paint(t, m, 80, 12)
		return strings.Contains(out, "34k context")
	})
	for _, want := range []string{"worker", "opus-5", "41% from cache", "1.2k thinking"} {
		if !strings.Contains(out, want) {
			t.Errorf("the row does not mention %q:\n%s", want, out)
		}
	}

	// Too narrow for everything, the least important figure is dropped whole
	// rather than truncated into a word that teaches nothing.
	tight := paint(t, m, 68, 12)
	if !strings.Contains(tight, "34k context") {
		t.Errorf("a tight row lost the context count:\n%s", tight)
	}
	if strings.Contains(tight, "thinki…") || strings.Contains(tight, "cach…") {
		t.Errorf("a tight row was cut mid-word:\n%s", tight)
	}
}
