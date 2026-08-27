package alert_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"claudecontrol/internal/alert"
)

// standIn puts a fake notify-send first on PATH and returns the file it writes
// its arguments to. Without it a test run would post real notifications to the
// desktop of whoever is running it.
func standIn(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "posted")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + out + "\n"
	if err := os.WriteFile(filepath.Join(dir, "notify-send"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return out
}

// safeBuffer is written by the notifier and read by the test.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// The bell is one byte and it is the part that works over ssh.
func TestTheBellRings(t *testing.T) {
	standIn(t)
	var term safeBuffer
	n := alert.New(alert.Options{Bell: true}, &term)
	if err := n.Waiting("title", "body"); err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	if got := term.String(); got != "\a" {
		t.Errorf("the terminal received %q, want a bell", got)
	}
}

// A configuration that asked for nothing gets nothing.
func TestNothingAskedForNothingSent(t *testing.T) {
	posted := standIn(t)
	var term safeBuffer
	n := alert.New(alert.Options{}, &term)
	if err := n.Waiting("title", "body"); err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	if got := term.String(); got != "" {
		t.Errorf("the terminal received %q with the bell off", got)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(posted); err == nil {
		t.Error("a notification was posted with notifications off")
	}
}

// The notification carries what is waiting, so a screen of conversations tells
// you which one.
func TestTheNotificationNamesTheConversation(t *testing.T) {
	posted := standIn(t)
	n := alert.New(alert.Options{Desktop: true}, nil)
	if err := n.Waiting("Claude is waiting", "api is waiting on you"); err != nil {
		t.Fatalf("Waiting: %v", err)
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(posted)
		if err == nil && strings.Contains(string(raw), "api is waiting on you") {
			if !strings.Contains(string(raw), "Claude is waiting") {
				t.Errorf("posted without its title:\n%s", raw)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	raw, _ := os.ReadFile(posted)
	t.Errorf("nothing was posted; the file holds %q", raw)
}

// A machine with no notification program says so, rather than dropping the
// alert quietly.
func TestNoHelperIsReported(t *testing.T) {
	// A PATH holding nothing at all: LookPath then finds none of the helpers.
	t.Setenv("PATH", t.TempDir())
	n := alert.New(alert.Options{Desktop: true}, nil)
	if _, ok := n.Helper(); ok {
		t.Fatal("a helper was found on an empty PATH")
	}
	if err := n.Waiting("title", "body"); !errors.Is(err, alert.ErrNoHelper) {
		t.Errorf("Waiting returned %v, want ErrNoHelper", err)
	}
}

// The zero notifier is what a caller with nothing configured holds. It must be
// safe to ring.
func TestANilNotifierIsSilentRatherThanFatal(t *testing.T) {
	var n *alert.Notifier
	if err := n.Waiting("title", "body"); err != nil {
		t.Errorf("Waiting on a nil notifier returned %v", err)
	}
}
