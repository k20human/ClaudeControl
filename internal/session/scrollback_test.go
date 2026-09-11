package session_test

import (
	"runtime"
	"strings"
	"testing"

	"claudecontrol/internal/session"
)

// heapCost is what a session's kept history costs, in bytes, after n lines of
// eighty columns have scrolled off it.
func heapCost(t *testing.T, id string, lines int) (float64, int) {
	t.Helper()
	s, err := session.Attach(session.ID(id), 200, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var b strings.Builder
	for i := 0; i < lines; i++ {
		b.WriteString(strings.Repeat("x", 80) + "\r\n")
	}
	text := b.String()

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	s.Feed([]byte(text))
	runtime.GC()
	runtime.ReadMemStats(&after)

	return float64(after.HeapAlloc) - float64(before.HeapAlloc), s.History()
}

// A pane keeps what it was told to keep and no more.
//
// This is the largest thing the application holds. The emulator's own default
// is ten thousand lines, which at the measured 9.9 KB a line is 94 MiB for one
// session — and a window holding three conversations and six services reached
// a gigabyte, which is where this came from.
func TestAPaneKeepsWhatItWasToldToKeep(t *testing.T) {
	t.Cleanup(func() { session.SetDefaultScrollback(session.DefaultScrollback) })

	session.SetDefaultScrollback(500)
	cost, kept := heapCost(t, "short", 4000)
	if kept > 500 {
		t.Errorf("kept %d lines, want no more than 500", kept)
	}
	// Ten kilobytes a line is the measured cost; twice that is room for the
	// slice headers and whatever the allocator rounded up, and still far below
	// what four thousand lines would have cost.
	if max := 500 * 20 << 10; cost > float64(max) {
		t.Errorf("500 lines cost %.1f MiB, want under %d MiB",
			cost/(1<<20), max>>20)
	}
	t.Logf("500 lines: %.1f MiB (%d kept)", cost/(1<<20), kept)

	// And the default is the one this application chose, not the emulator's.
	session.SetDefaultScrollback(session.DefaultScrollback)
	_, keptDefault := heapCost(t, "default", session.DefaultScrollback+2000)
	if keptDefault > session.DefaultScrollback {
		t.Errorf("the default kept %d lines, want %d", keptDefault, session.DefaultScrollback)
	}
}

// Nothing kept is a setting, not a failure — and it is the number the
// emulator underneath cannot be given: it reads zero as "use my default", and
// its default is the ten thousand lines this exists to avoid. Asking for
// nothing must not quietly ask for the most there is.
func TestAskingForNothingDoesNotAskForEverything(t *testing.T) {
	t.Cleanup(func() { session.SetDefaultScrollback(session.DefaultScrollback) })

	session.SetDefaultScrollback(0)
	cost, kept := heapCost(t, "none", 4000)
	if kept > 1 {
		t.Errorf("kept %d lines, want next to none", kept)
	}
	if cost > 8<<20 {
		t.Errorf("keeping nothing still cost %.1f MiB", cost/(1<<20))
	}
	t.Logf("nothing kept: %.1f MiB (%d lines)", cost/(1<<20), kept)
}
