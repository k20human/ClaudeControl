package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/transcript"
)

func transcriptApp(t *testing.T) *App {
	t.Helper()
	isolateState(t)
	b := bus.New()
	return &App{
		root:     &layout.Node{Kind: layout.KindLeaf, PaneID: 1},
		modules:  map[layout.PaneID]module.Module{1: &stub{}},
		bus:      b,
		pool:     pool.New(b),
		rects:    map[layout.PaneID]layout.Rect{1: {X: 0, Y: 0, W: 80, H: 24}},
		area:     layout.Rect{X: 0, Y: 0, W: 80, H: 24},
		focus:    1,
		prev:     1,
		nextPane: 1,
		hoverDiv: -1,
		hoverBtn: -1,
		pointerX: -1,
		pointerY: -1,
		wake:     make(chan struct{}, 1),
	}
}

func TestFollowingATranscriptPublishesMetrics(t *testing.T) {
	a := transcriptApp(t)
	defer a.stopTranscripts()

	ch, _ := a.bus.SubscribeEvent(transcript.SessionTopic, transcript.SessionDepth)
	p := filepath.Join(t.TempDir(), "t.jsonl")
	body := `{"type":"assistant","message":{"model":"claude-opus-5","usage":{"input_tokens":2,"cache_read_input_tokens":98,"output_tokens":5}}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	a.followTranscript("abc", p)

	select {
	case v := <-ch:
		sm, ok := v.(transcript.SessionMetrics)
		if !ok {
			t.Fatalf("published %T, want SessionMetrics", v)
		}
		if sm.SessionID != "abc" {
			t.Errorf("SessionID = %q, want abc", sm.SessionID)
		}
		if sm.Metrics.Context != 100 {
			t.Errorf("Context = %d, want 100", sm.Metrics.Context)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was published")
	}
}

// One tailer per session. A hook fires many times a turn, and starting a
// tailer on each would leave a goroutine and an open file behind every time.
func TestFollowingTheSameSessionTwiceStartsOneTailer(t *testing.T) {
	a := transcriptApp(t)
	defer a.stopTranscripts()

	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.followTranscript("abc", p)
	a.followTranscript("abc", p)
	a.followTranscript("abc", p)

	if n := len(a.tailers); n != 1 {
		t.Fatalf("%d tailers running, want 1", n)
	}
}
