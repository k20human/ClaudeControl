package session_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/vt"

	"claudecontrol/internal/session"
)

// longTail pads a title past the length a terminal might be willing to buffer.
var longTail = strings.Repeat("x", 600)

// The same thing through a real process and a real pty, because that is where
// it was seen: a glyph in the title, and the rest of the title printed across
// the guest's own prompt.
func TestAGuestsTitleNeverReachesItsScreen(t *testing.T) {
	for _, tc := range []struct{ name, seq string }{
		{"OSC 2 with BEL", `\033]2;Cat-543 rebase sur preprod\007`},
		{"OSC 0 with BEL", `\033]0;Cat-543 rebase sur preprod\007`},
		{"OSC 2 with ST", `\033]2;Cat-543 rebase sur preprod\033\\`},
		{"a glyph in the title", `\033]2;\342\234\263 Cat-543 rebase sur preprod\007`},
		{"an em dash in the title", `\033]2;\342\227\220 2 waiting \342\200\224 Cat-543 rebase sur preprod\007`},
		{"a long title", `\033]2;Cat-543 rebase sur preprod ` + longTail + `\007`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var named []string
			s, err := session.Start(session.Spec{
				ID:   "title",
				Argv: []string{"sh", "-c", `printf '` + tc.seq + `ready'; cat`},
				Dir:  t.TempDir(),
				Callbacks: vt.Callbacks{Title: func(name string) {
					mu.Lock()
					named = append(named, name)
					mu.Unlock()
				}},
				Width:  40,
				Height: 4,
			})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer s.Close()

			waitFor(t, `"ready" on screen`, func() bool {
				return anyRowContains(s, 40, 4, "ready")
			})
			if anyRowContains(s, 40, 4, "Cat-543") {
				g := snapshot(s, 40, 4)
				t.Errorf("the title was printed into the pane:\nrow0=%q\nrow1=%q", g.row(0), g.row(1))
			}

			// It still arrives as a title. Dropping it would have been the
			// easy fix, and would have cost the host any chance of showing
			// what a guest calls itself.
			waitFor(t, "the title to reach the host", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(named) > 0
			})
			mu.Lock()
			got := named
			mu.Unlock()
			if !strings.Contains(strings.Join(got, "|"), "Cat-543 rebase sur preprod") &&
				!strings.Contains(strings.Join(got, "|"), "subject") {
				t.Errorf("the host was told %q", got)
			}
		})
	}
}

// A service kept running across a restart has its log replayed into a screen,
// and a service names its window as readily as anything else. The title must
// not arrive as text across the log.
func TestATitleInAReplayedLogNeverReachesTheScreen(t *testing.T) {
	s, err := session.Attach("kept", 40, 4, nil)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer s.Close()

	// Split where it hurts: inside the glyph that breaks the parser.
	whole := []byte("\x1b]2;✳ npm run dev\x07serving on 5173")
	s.Feed(whole[:8])
	s.Feed(whole[8:])

	if !anyRowContains(s, 40, 4, "serving on 5173") {
		t.Fatalf("the log itself was lost:\n%q", snapshot(s, 40, 4).row(0))
	}
	if anyRowContains(s, 40, 4, "npm run dev") {
		t.Errorf("the title was printed into the pane: %q", snapshot(s, 40, 4).row(0))
	}
}
