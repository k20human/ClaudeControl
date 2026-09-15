package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// conversationTabs is a pane of tabs holding a conversation beside a shell.
// named is what the conversation calls itself once its transcript says so,
// clipped to what a shared strip has room for. The tab wears this, not the
// title in the file — which is the whole point of the naming.
const named = "Analyse Wayl"

func conversationTabs(t *testing.T) (cfg string, env []string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "DEV")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := fakeClaude(t, t.TempDir(), "Analyse Wayland")

	cfg = filepath.Join(t.TempDir(), "closing.yaml")
	body := `layout:
  module: tabs
  options:
    tabs:
      - title: talk
        module: claude
        options:
          bin: ` + bin + `
          dir: ` + work + `
      - title: shell
        module: term
        options: { cmd: [sh, -c, "printf SHELL-HERE; cat"] }
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg, []string{"CLAUDE_CONFIG_DIR=" + t.TempDir()}
}

// crossOf is where the cross of the tab on screen was drawn.
func crossOf(t *testing.T, snap func() *screen) int {
	t.Helper()
	x := columnOf(snap().row(0), "×")
	if x < 0 {
		t.Fatalf("no cross in the strip: %q", snap().row(0))
	}
	return x
}

// Closing a tab that holds a conversation asks what to do with it.
//
// It used to detach without a word, which is right when you meant to make room
// and wrong when you meant to be done — and three conversations left running
// in an afternoon showed that the gesture cannot say which.
func TestClosingATabWithAConversationAsksFirst(t *testing.T) {
	const W, H = 100, 14
	cfg, env := conversationTabs(t)
	s, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "FAKE-CLAUDE")
	waitForAnywhere(t, snap, named)

	click(t, s, 5, 5) // focus
	click(t, s, crossOf(t, snap), 0)

	waitForAnywhere(t, snap, "CLOSE")
	// And nothing has happened yet.
	if row := snap().row(0); !strings.Contains(row, named) {
		t.Errorf("the tab went before the question was answered: %q", row)
	}

	// Escape leaves everything as it was.
	s.SendText("\x1b")
	waitFor(t, snap, func(g *screen) bool {
		return strings.Contains(g.row(0), named) && !strings.Contains(allRows(g), "CLOSE")
	}, "the tab to stay")
}

// Enter closes the tab and leaves the conversation running, which is what it
// always did — now on purpose.
func TestEnterClosesTheTabAndKeepsTheConversation(t *testing.T) {
	const W, H = 100, 14
	cfg, env := conversationTabs(t)
	s, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "FAKE-CLAUDE")
	waitForAnywhere(t, snap, named)

	click(t, s, 5, 5)
	click(t, s, crossOf(t, snap), 0)
	waitForAnywhere(t, snap, "CLOSE")
	s.SendText("\r")

	waitFor(t, snap, func(g *screen) bool { return !strings.Contains(g.row(0), named) },
		"the tab to go")

	// The conversation is still there, in the list that exists for it.
	s.SendText("\x1b ") // alt+space
	waitForAnywhere(t, snap, "detached")
}

// And x closes the tab and ends the conversation with it.
func TestXClosesTheTabAndEndsTheConversation(t *testing.T) {
	const W, H = 100, 14
	cfg, env := conversationTabs(t)
	s, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "FAKE-CLAUDE")
	waitForAnywhere(t, snap, named)

	click(t, s, 5, 5)
	click(t, s, crossOf(t, snap), 0)
	waitForAnywhere(t, snap, "CLOSE")
	s.SendText("x")

	waitFor(t, snap, func(g *screen) bool { return !strings.Contains(g.row(0), named) },
		"the tab to go")

	// Nothing left to come back to: the list holds no conversation at all.
	s.SendText("\x1b ")
	waitFor(t, snap, func(g *screen) bool {
		return strings.Contains(allRows(g), "SESSIONS") && !strings.Contains(allRows(g), "detached")
	}, "an empty list")
}

// A tab holding a shell closes without a question. There is nothing to keep.
func TestClosingAShellTabAsksNothing(t *testing.T) {
	const W, H = 100, 14
	cfg, env := conversationTabs(t)
	s, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "FAKE-CLAUDE")
	waitForAnywhere(t, snap, named)

	click(t, s, 5, 5)
	// Move to the shell, whose cross is then the one on screen.
	s.SendText("\x1b'") // alt+' — next tab
	waitForAnywhere(t, snap, "SHELL-HERE")

	click(t, s, crossOf(t, snap), 0)
	waitFor(t, snap, func(g *screen) bool {
		return !strings.Contains(g.row(0), "shell") && !strings.Contains(allRows(g), "CLOSE")
	}, "the shell tab to go without a question")
}

// allRows is the whole screen as one string, for a panel whose position
// depends on the window size.
func allRows(g *screen) string {
	var b strings.Builder
	for y := 0; y < g.h; y++ {
		b.WriteString(g.row(y))
		b.WriteByte('\n')
	}
	return b.String()
}
