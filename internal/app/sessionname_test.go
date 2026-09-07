package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claudecontrol/internal/transcript"
)

// The name Claude Code gives a conversation has to travel from the transcript
// to whatever is showing that conversation: the pane title, and the tab strip
// that replaced it. This is the application's half of that — the strip's half
// is a test of its own, and between them they cover the chain.
func TestTheNameInATranscriptReachesTheApplication(t *testing.T) {
	a := newTestApp(t)
	a.bus = testBus
	names := a.bus.SubscribeState(transcript.NameTopic)

	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	body := `{"type":"user","cwd":"/home/k20/DEV","message":{"content":"hello"}}` + "\n" +
		`{"type":"assistant","aiTitle":"Interface visuelle Claude Code","message":{"content":[]}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	a.followTranscript("pane-uuid", path)
	t.Cleanup(func() {
		a.tailerMu.Lock()
		for _, tl := range a.tailers {
			tl.Close()
		}
		a.tailerMu.Unlock()
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.sessionName("pane-uuid") == "Interface visuelle Claude Code" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if got := a.sessionName("pane-uuid"); got != "Interface visuelle Claude Code" {
		t.Fatalf("the application knows the session as %q", got)
	}

	// And it says so, because a pane of tabs cannot read the transcript
	// itself: it hears the name or it keeps the directory's.
	select {
	case v := <-names:
		named, ok := v.(transcript.SessionName)
		if !ok {
			t.Fatalf("published %T on the name topic", v)
		}
		if named.SessionID != "pane-uuid" || named.Name != "Interface visuelle Claude Code" {
			t.Errorf("published %+v", named)
		}
	case <-time.After(3 * time.Second):
		t.Error("the name was never published")
	}
}
