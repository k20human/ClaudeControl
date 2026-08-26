package app

import (
	"time"

	"claudecontrol/internal/transcript"
)

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
			if line.AITitle != "" {
				a.usageMu.Lock()
				if a.names == nil {
					a.names = make(map[string]string)
				}
				a.names[sessionID] = line.AITitle
				a.usageMu.Unlock()
				a.bus.PublishState(transcript.NameTopic, transcript.SessionName{
					SessionID: sessionID, Name: line.AITitle,
				})
				a.Wake()
			}
			if line.Type != "assistant" {
				continue
			}
			m := transcript.Derive(line.Model, line.Usage, windows)
			a.usageMu.Lock()
			if a.usage == nil {
				a.usage = make(map[string]transcript.Metrics)
			}
			a.usage[sessionID] = m
			a.usageMu.Unlock()
			a.bus.PublishState(transcript.SessionTopic, transcript.SessionMetrics{
				SessionID: sessionID,
				Metrics:   m,
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
