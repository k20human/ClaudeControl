package hooks_test

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"claudecontrol/internal/hooks"
)

func TestListenerReceivesAPayload(t *testing.T) {
	l, err := hooks.Listen(t.TempDir())
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	body := `{"session_id":"abc","cwd":"/tmp","transcript_path":"/t.jsonl","permission_mode":"auto"}`
	if err := hooks.Send(l.Path(), "Notification", "", strings.NewReader(body)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case p := <-l.Events():
		if p.Event != "Notification" {
			t.Errorf("Event = %q, want Notification", p.Event)
		}
		if p.SessionID != "abc" {
			t.Errorf("SessionID = %q, want abc", p.SessionID)
		}
		if p.TranscriptPath != "/t.jsonl" {
			t.Errorf("TranscriptPath = %q, want /t.jsonl", p.TranscriptPath)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the payload never arrived")
	}
}

// A hook runs inside Claude Code's own process tree. It must never be able to
// take the application down with it, however malformed its payload.
func TestMalformedPayloadIsIgnoredAndTheListenerSurvives(t *testing.T) {
	l, err := hooks.Listen(t.TempDir())
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	// Written straight to the socket rather than through Send: Send sanitises
	// what it is given, so a payload that will not parse leaves it as a valid
	// message with empty fields. The listener's own guard needs a genuinely
	// broken line to be exercised at all.
	conn, err := net.Dial("unix", l.Path())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := conn.Write([]byte("{not json\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.Close()

	body := `{"session_id":"ok"}`
	if err := hooks.Send(l.Path(), "Stop", "", strings.NewReader(body)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case p := <-l.Events():
		if p.SessionID != "ok" {
			t.Fatalf("SessionID = %q, want the well-formed payload to come through", p.SessionID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the listener stopped after a malformed payload")
	}
}

// The settings string is handed to Claude Code with --settings, which merges
// it with the user's own configuration. It must therefore carry hooks and
// nothing else.
func TestSettingsJSONCarriesOnlyOurHooks(t *testing.T) {
	got, err := hooks.SettingsJSON("/usr/local/bin/claudecontrol", "/run/user/1000/cc.sock", "pane-1")
	if err != nil {
		t.Fatalf("SettingsJSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("the settings are not valid JSON: %v\n%s", err, got)
	}
	if len(doc) != 1 {
		t.Fatalf("settings hold %d keys, want only \"hooks\"", len(doc))
	}
	h, ok := doc["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("settings = %s, want a hooks object", got)
	}
	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop", "SessionEnd"} {
		if _, ok := h[event]; !ok {
			t.Errorf("no hook registered for %q", event)
		}
	}
	if !strings.Contains(got, "/usr/local/bin/claudecontrol") {
		t.Error("the hook command does not point at our own binary")
	}
}

// A hook that cannot reach the socket must fail quietly rather than hold
// Claude Code up.
func TestSendToADeadSocketFailsFast(t *testing.T) {
	start := time.Now()
	err := hooks.Send(t.TempDir()+"/absent.sock", "Stop", "", strings.NewReader("{}"))
	if err == nil {
		t.Fatal("Send to a dead socket returned no error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Send took %v against a dead socket; it must not hold a hook up", d)
	}
}

// The pane's own identity travels on the hook's command line, not in the
// payload: the payload's session id is Claude Code's, and Claude Code changes
// it — resuming a conversation writes a new transcript under a new id.
func TestTheSettingsCarryThePaneIdentity(t *testing.T) {
	got, err := hooks.SettingsJSON("/usr/bin/claudecontrol", "/run/sock", "pane-abc")
	if err != nil {
		t.Fatalf("SettingsJSON: %v", err)
	}
	if !strings.Contains(got, "--pane pane-abc") {
		t.Errorf("the hook command does not name the pane: %s", got)
	}
	if !strings.Contains(got, "--hook Notification") {
		t.Errorf("the hook command lost its event: %s", got)
	}
}

// And it reaches the other end, beside whatever Claude Code says its session
// is now.
func TestAForwardedHookCarriesBoth(t *testing.T) {
	dir := t.TempDir()
	l, err := hooks.Listen(dir)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	body := `{"session_id":"claude-current","transcript_path":"/tmp/t.jsonl"}`
	if err := hooks.Send(l.Path(), "Notification", "pane-abc", strings.NewReader(body)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case p := <-l.Events():
		if p.Pane != "pane-abc" {
			t.Errorf("Pane = %q, want the identity we gave the session", p.Pane)
		}
		if p.SessionID != "claude-current" {
			t.Errorf("SessionID = %q, want what Claude Code says it is now", p.SessionID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nothing arrived")
	}
}
