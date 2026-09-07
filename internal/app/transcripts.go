package app

import (
	"time"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/transcript"
)

// followTranscript starts following a session's transcript, once per file.
//
// A hook fires several times a turn and carries the same path every time, so
// this has to be idempotent: starting a tailer per hook would leak a goroutine
// and an open file on each one.
//
// A path that changes is a different matter. Resuming a conversation makes
// Claude Code write a new transcript under a new id, and the hook is what says
// which — so the tailer follows the new file and lets the old one go. Keeping
// the first one would mean reporting, for the rest of the run, the turns of a
// conversation that had moved on.
func (a *App) followTranscript(sessionID, path string) {
	if sessionID == "" || path == "" {
		return
	}
	a.tailerMu.Lock()
	defer a.tailerMu.Unlock()
	if a.tailers == nil {
		a.tailers = make(map[string]*transcript.Tailer)
		a.tailed = make(map[string]string)
	}
	if tl, running := a.tailers[sessionID]; running {
		if a.tailed[sessionID] == path {
			return
		}
		tl.Close()
	}

	tl := transcript.Tail(path, 250*time.Millisecond)
	a.tailers[sessionID] = tl
	a.tailed[sessionID] = path

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
				a.bus.PublishEvent(transcript.NameTopic, transcript.SessionName{
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
			a.bus.PublishEvent(transcript.SessionTopic, transcript.SessionMetrics{
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
	a.tailed = nil
}

// followHeldSessions starts reading the transcript of every conversation the
// panes hold.
//
// Nothing else did. A transcript is followed when a hook says where it is, and
// no hook fires until you speak to a session — so a conversation brought back
// from the last run had a name, a model and a token count sitting in a file on
// disk, and the application knew none of it until you typed something. Which
// is exactly what a restored tab reading the name of its directory looks like.
//
// What it reads is the transcript of the conversation as it was left. That is
// the truth about it until the next turn replaces it, and a name from
// yesterday beats no name at all.
func (a *App) followHeldSessions() {
	for _, id := range layout.Leaves(a.root) {
		m, ok := a.modules[id]
		if !ok {
			continue
		}
		lister, ok := m.(module.Sessioner)
		if !ok {
			continue
		}
		for _, sess := range lister.Sessions() {
			if path, found := transcript.Find(sess); found {
				a.followTranscript(sess, path)
			}
		}
	}
}
