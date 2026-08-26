package probe_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/probe"
)

func check(t *testing.T, p probe.Probe) (probe.Health, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return p.Check(ctx)
}

// A command probe reports on its exit status, which is the only thing a shell
// command reliably says about itself.
func TestCommandProbeFollowsTheExitStatus(t *testing.T) {
	if h, _ := check(t, probe.Command("ok", []string{"true"})); h != probe.HealthUp {
		t.Errorf("true reported %v, want up", h)
	}
	if h, _ := check(t, probe.Command("bad", []string{"false"})); h != probe.HealthDown {
		t.Errorf("false reported %v, want down", h)
	}
}

// A command that is not installed is down with a reason, not a crash.
func TestCommandProbeReportsAMissingCommand(t *testing.T) {
	h, detail := check(t, probe.Command("absent", []string{"definitely-not-installed-xyz"}))
	if h != probe.HealthDown {
		t.Errorf("a missing command reported %v, want down", h)
	}
	if detail == "" {
		t.Error("a missing command reported no reason")
	}
}

// The detail is what the operator reads, so a failing command's own output is
// worth more than a generic message.
func TestCommandProbeCarriesTheOutput(t *testing.T) {
	_, detail := check(t, probe.Command("noisy", []string{"sh", "-c", "echo the-reason >&2; exit 1"}))
	if !strings.Contains(detail, "the-reason") {
		t.Errorf("detail = %q, want the command's own output", detail)
	}
}

func TestHTTPProbeFollowsTheStatusCode(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()

	if h, _ := check(t, probe.HTTP("up", up.URL)); h != probe.HealthUp {
		t.Errorf("200 reported %v, want up", h)
	}
	if h, d := check(t, probe.HTTP("down", down.URL)); h != probe.HealthDown {
		t.Errorf("500 reported %v (%s), want down", h, d)
	}
}

func TestHTTPProbeReportsAnUnreachableHost(t *testing.T) {
	h, detail := check(t, probe.HTTP("gone", "http://127.0.0.1:1/health"))
	if h != probe.HealthDown {
		t.Errorf("an unreachable host reported %v, want down", h)
	}
	if detail == "" {
		t.Error("an unreachable host reported no reason")
	}
}

func TestTCPProbeFollowsWhetherItCanConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if h, d := check(t, probe.TCP("open", ln.Addr().String())); h != probe.HealthUp {
		t.Errorf("an open port reported %v (%s), want up", h, d)
	}
	if h, _ := check(t, probe.TCP("shut", "127.0.0.1:1")); h != probe.HealthDown {
		t.Errorf("a closed port reported %v, want down", h)
	}
}

// A probe must honour the deadline it is given rather than hold the runner up.
func TestAProbeHonoursItsDeadline(t *testing.T) {
	p := probe.Command("slow", []string{"sleep", "10"})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	h, _ := p.Check(ctx)
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the probe took %v despite a 200ms deadline", d)
	}
	if h != probe.HealthDown {
		t.Errorf("a probe that ran out of time reported %v, want down", h)
	}
}
