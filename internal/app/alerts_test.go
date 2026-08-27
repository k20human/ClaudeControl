package app

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"claudecontrol/internal/alert"
	"claudecontrol/internal/pool"
)

type ringBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.Write(p)
}

func (r *ringBuffer) rings() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Count(r.b.String(), "\a")
}

func alertApp(t *testing.T) (*App, *ringBuffer) {
	t.Helper()
	a := newTestApp(t)
	var term ringBuffer
	a.alerts = alert.New(alert.Options{Bell: true}, &term)
	a.waitingMark = make(map[string]bool)
	return a, &term
}

// Hooks arrive whenever Claude Code has something to report. A bell on each of
// them would be an alarm rather than a notice: what is announced is the moment
// the answer stopped being Claude's and became yours.
func TestASessionAnnouncesItselfOnceWhenItStartsWaiting(t *testing.T) {
	a, term := alertApp(t)

	a.noteAlert("pane-1", pool.StateWaiting)
	if n := term.rings(); n != 1 {
		t.Fatalf("%d bells for the first notification, want 1", n)
	}
	a.noteAlert("pane-1", pool.StateWaiting)
	a.noteAlert("pane-1", pool.StateWaiting)
	if n := term.rings(); n != 1 {
		t.Errorf("%d bells while it stayed waiting, want 1", n)
	}
}

// And again once it has gone back to work and stopped a second time: the
// second wait is as much news as the first.
func TestWaitingAgainAnnouncesAgain(t *testing.T) {
	a, term := alertApp(t)

	a.noteAlert("pane-1", pool.StateWaiting)
	a.noteAlert("pane-1", pool.StateWorking)
	a.noteAlert("pane-1", pool.StateWaiting)
	if n := term.rings(); n != 2 {
		t.Errorf("%d bells for two separate waits, want 2", n)
	}
}

// Two conversations are two pieces of news.
func TestEachConversationAnnouncesItself(t *testing.T) {
	a, term := alertApp(t)

	a.noteAlert("pane-1", pool.StateWaiting)
	a.noteAlert("pane-2", pool.StateWaiting)
	if n := term.rings(); n != 2 {
		t.Errorf("%d bells for two conversations, want 2", n)
	}
}

// Working and idle are not news.
func TestOnlyWaitingAnnouncesItself(t *testing.T) {
	a, term := alertApp(t)

	a.noteAlert("pane-1", pool.StateWorking)
	a.noteAlert("pane-1", pool.StateIdle)
	if n := term.rings(); n != 0 {
		t.Errorf("%d bells for states nobody has to act on, want 0", n)
	}
}

// The bell is raised on the hook pump's goroutine while the drawing loop is
// writing frames to the same terminal. A byte landing in the middle of an
// escape sequence would corrupt the screen, so what the notifier writes is
// recorded and rung by the loop that owns the terminal.
func TestTheBellIsRungByTheDrawingLoop(t *testing.T) {
	a := newTestApp(t)
	a.waitingMark = make(map[string]bool)
	a.alerts = alert.New(alert.Options{Bell: true}, bellSink{a})

	a.noteAlert("pane-1", pool.StateWaiting)
	if !a.bellPending.Load() {
		t.Fatal("the bell was not recorded for the drawing loop")
	}

	// With no terminal open yet it must clear the mark rather than panic or
	// hold it forever: hooks arrive before the first frame.
	a.ringPendingBell()
	if a.bellPending.Load() {
		t.Error("the mark survived being rung")
	}
}
