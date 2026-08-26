package app

import (
	"time"

	"claudecontrol/internal/transcript"
)

// UsageTopic is where per-session metrics are published.
const UsageTopic = "session.usage"

// SessionMetrics ties a turn's metrics to the session that produced them.
type SessionMetrics struct {
	SessionID string
	Metrics   transcript.Metrics
}

// followTranscript starts following a session's transcript, once.
//
// A hook fires several times a turn and carries the same path every time, so
// this has to be idempotent: starting a tailer per hook would leak a goroutine
// and an open file on each one.
func (a *App) followTranscript(sessionID, path string) {
	if sessionID == "" || path == "" {
		return
	}
	a.tailerMu.Lock()
	defer a.tailerMu.Unlock()
	if a.tailers == nil {
		a.tailers = make(map[string]*transcript.Tailer)
	}
	if _, running := a.tailers[sessionID]; running {
		return
	}

	tl := transcript.Tail(path, 250*time.Millisecond)
	a.tailers[sessionID] = tl

	windows := transcript.DefaultWindows()
	go func() {
		for line := range tl.Lines() {
			if line.Type != "assistant" {
				continue
			}
			a.bus.PublishState(UsageTopic, SessionMetrics{
				SessionID: sessionID,
				Metrics:   transcript.Derive(line.Model, line.Usage, windows),
			})
			a.Wake()
		}
	}()
}

// stopTranscripts closes every follower.
func (a *App) stopTranscripts() {
	a.tailerMu.Lock()
	defer a.tailerMu.Unlock()
	for _, tl := range a.tailers {
		tl.Close()
	}
	a.tailers = nil
}
