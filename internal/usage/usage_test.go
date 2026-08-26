package usage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/usage"
)

// payload is the shape the endpoint answered with on 2026-08-26, trimmed to
// what this package reads. Keeping a real sample means a change of shape shows
// up as a failing test rather than as a blank panel.
const payload = `{
  "five_hour": {"utilization": 37.0, "resets_at": "2026-08-26T14:19:59.453672+00:00"},
  "seven_day": {"utilization": 14.0, "resets_at": "2026-09-02T05:59:59.453693+00:00"},
  "limits": [
    {"kind": "session", "percent": 37, "resets_at": "2026-08-26T14:19:59.453672+00:00"},
    {"kind": "weekly_all", "percent": 14, "resets_at": "2026-09-02T05:59:59.453693+00:00"},
    {"kind": "weekly_scoped", "percent": 3, "resets_at": null, "scope": {"model": {"display_name": "Fable"}}}
  ]
}`

func credentials(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	return path
}

// liveToken writes credentials whose token has an hour left.
func liveToken(t *testing.T) string {
	t.Helper()
	return credentials(t, tokenJSON("tok-abc", time.Now().Add(time.Hour)))
}

func tokenJSON(value string, expires time.Time) string {
	return `{"claudeAiOauth":{"accessToken":"` + value + `","expiresAt":` +
		strconv.FormatInt(expires.UnixMilli(), 10) + `}}`
}

func TestFetchReadsBothWindowsAndThePerModelCaps(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := &usage.Client{Endpoint: srv.URL, CredentialsPath: liveToken(t)}
	snap, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
	if snap.FiveHour.Percent != 37 || !snap.FiveHour.Resets {
		t.Errorf("five hour = %+v", snap.FiveHour)
	}
	if snap.SevenDay.Percent != 14 || !snap.SevenDay.Resets {
		t.Errorf("seven day = %+v", snap.SevenDay)
	}
	// The account-wide entries repeat the two windows, so only the per-model
	// cap should survive.
	if len(snap.Scoped) != 1 || snap.Scoped[0].Model != "Fable" || snap.Scoped[0].Percent != 3 {
		t.Fatalf("scoped = %+v", snap.Scoped)
	}
	// A null reset time must not become 1 January year 1.
	if snap.Scoped[0].Resets {
		t.Errorf("a null resets_at was taken for a real time: %v", snap.Scoped[0].ResetsAt)
	}
}

// A response whose shape has drifted must be reported as unavailable. Zero
// percent used and no reading at all are different facts, and a panel showing
// "0%" when it means "I could not tell" is worse than one that says so.
func TestAResponseWithoutBudgetsIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"something_else": 1}`))
	}))
	defer srv.Close()

	c := &usage.Client{Endpoint: srv.URL, CredentialsPath: liveToken(t)}
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Fatal("a response with no budget was accepted")
	} else if !strings.Contains(err.Error(), "shape has changed") {
		t.Errorf("error = %v, want it to name the changed shape", err)
	}
}

func TestARefusedRequestNamesTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := &usage.Client{Endpoint: srv.URL, CredentialsPath: liveToken(t)}
	_, err := c.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("error = %v, want the status in it", err)
	}
}

// An expired token earns a 401 and nothing else, so it is worth naming before
// the request is made.
func TestAnExpiredTokenIsReportedWithoutAsking(t *testing.T) {
	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		asked = true
	}))
	defer srv.Close()

	path := credentials(t, `{"claudeAiOauth":{"accessToken":"tok","expiresAt":1000}}`)
	c := &usage.Client{Endpoint: srv.URL, CredentialsPath: path}
	_, err := c.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %v, want it to say the token expired", err)
	}
	if asked {
		t.Error("the endpoint was called with a dead token")
	}
}

// The error must be usable without leaking the credential it describes.
func TestErrorsNeverCarryTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	path := credentials(t, tokenJSON("super-secret-value", time.Now().Add(time.Hour)))
	c := &usage.Client{Endpoint: srv.URL, CredentialsPath: path}
	_, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "super-secret-value") {
		t.Errorf("the error carries the token: %v", err)
	}
}

func TestMissingCredentialsAreNamedNotGuessed(t *testing.T) {
	c := &usage.Client{Endpoint: "http://127.0.0.1:1", CredentialsPath: filepath.Join(t.TempDir(), "nope.json")}
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Fatal("a missing credentials file was accepted")
	}
}

func TestDefaultCredentialsPathFollowsTheConfigOverride(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/somewhere/else")
	if got, want := usage.DefaultCredentialsPath(), "/somewhere/else/.credentials.json"; got != want {
		t.Errorf("DefaultCredentialsPath = %q, want %q", got, want)
	}
}

// A failed reading has to reach the display as a failure, not as silence.
func TestThePollerPublishesFailuresToo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	b := bus.New()
	defer b.Close()
	ch := b.SubscribeState(usage.Topic)

	woke := make(chan struct{}, 1)
	p := usage.NewPoller(
		&usage.Client{Endpoint: srv.URL, CredentialsPath: liveToken(t)},
		b, time.Hour, func() {
			select {
			case woke <- struct{}{}:
			default:
			}
		},
	)
	p.Start()
	defer p.Stop()

	select {
	case v := <-ch:
		r, ok := v.(usage.Reading)
		if !ok {
			t.Fatalf("published %T, want a Reading", v)
		}
		if r.Err == nil {
			t.Error("the reading carries no error")
		}
		if r.At.IsZero() {
			t.Error("the reading carries no time")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nothing was published")
	}

	select {
	case <-woke:
	case <-time.After(time.Second):
		t.Error("the display was not woken")
	}

	if last, taken := p.Last(); !taken || last.Err == nil {
		t.Errorf("Last = %+v, taken=%v", last, taken)
	}
}

func TestThePollerKeepsTheSnapshotItRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	p := usage.NewPoller(&usage.Client{Endpoint: srv.URL, CredentialsPath: liveToken(t)}, nil, time.Hour, nil)
	p.Refresh()
	last, taken := p.Last()
	if !taken {
		t.Fatal("no reading was taken")
	}
	if last.Err != nil {
		t.Fatalf("Refresh: %v", last.Err)
	}
	if last.Snapshot.FiveHour.Percent != 37 {
		t.Errorf("five hour = %v, want 37", last.Snapshot.FiveHour.Percent)
	}
}
