package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// workedIn writes a transcript saying a conversation happened in a directory,
// which is what puts that directory in the list.
func workedIn(t *testing.T, configDir, dir string) {
	t.Helper()
	project := filepath.Join(configDir, "projects", "-"+filepath.Base(dir))
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"type": "user", "cwd": dir,
		"message": map[string]any{"content": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, filepath.Base(dir)+"-id.jsonl")
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The panel offers the directories you have worked in, and opening one puts a
// session there — in a new tab, leaving what was running alone.
func TestOpeningASessionInADirectoryYouHaveWorkedIn(t *testing.T) {
	const W, H = 120, 22
	root := t.TempDir()
	cfgDir := filepath.Join(root, "claude")
	project := filepath.Join(root, "widgets")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	workedIn(t, cfgDir, project)

	s, snap := runWithEnv(t, twoTabbedPanes(t), W, H, vt.Callbacks{},
		[]string{"CLAUDE_CONFIG_DIR=" + cfgDir})
	waitForAnywhere(t, snap, "LEFT-HERE")
	click(t, s, 10, 5)

	rightClick(t, s, 10, 5)
	waitForAnywhere(t, snap, "open in…")
	x, y := at(t, snap, "open in…")
	click(t, s, x, y)

	waitForAnywhere(t, snap, "open a session in")
	waitForAnywhere(t, snap, "widgets")
}

// Typing a path offers the filesystem, so a directory you have never had a
// conversation in is reachable — and offered as something to click rather than
// as spelling to get right.
func TestTypingAPathOffersTheFilesystem(t *testing.T) {
	const W, H = 120, 22
	root := t.TempDir()
	never := filepath.Join(root, "never-worked-here")
	if err := os.MkdirAll(never, 0o700); err != nil {
		t.Fatal(err)
	}

	s, snap := runWithEnv(t, twoTabbedPanes(t), W, H, vt.Callbacks{},
		[]string{"CLAUDE_CONFIG_DIR=" + filepath.Join(root, "empty")})
	waitForAnywhere(t, snap, "LEFT-HERE")
	click(t, s, 10, 5)
	rightClick(t, s, 10, 5)
	waitForAnywhere(t, snap, "open in…")
	x, y := at(t, snap, "open in…")
	click(t, s, x, y)
	waitForAnywhere(t, snap, "open a session in")

	s.SendText(root + string(filepath.Separator))
	time.Sleep(300 * time.Millisecond)
	waitForAnywhere(t, snap, "never-worked-here")
}

// The panel is about where the next session opens. The one already running is
// not touched by it.
func TestChoosingADirectoryLeavesTheRunningSessionAlone(t *testing.T) {
	const W, H = 120, 22
	root := t.TempDir()
	cfgDir := filepath.Join(root, "claude")
	project := filepath.Join(root, "widgets")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	workedIn(t, cfgDir, project)

	s, snap := runWithEnv(t, twoTabbedPanes(t), W, H, vt.Callbacks{},
		[]string{"CLAUDE_CONFIG_DIR=" + cfgDir})
	waitForAnywhere(t, snap, "LEFT-HERE")
	click(t, s, 10, 5)
	rightClick(t, s, 10, 5)
	waitForAnywhere(t, snap, "open in…")
	x, y := at(t, snap, "open in…")
	click(t, s, x, y)
	waitForAnywhere(t, snap, "open a session in")

	// The row for that directory, which is below the query line — clicking
	// the query line would merely close the panel and prove nothing.
	wx, wy := rowFor(t, snap, "widgets")
	click(t, s, wx, wy)
	time.Sleep(800 * time.Millisecond)

	// A tab was added, not a pane taken over: what was open is still open,
	// and there is now one more.
	g := snap()
	left := stripHalf(g, false)
	if !strings.Contains(left, "leftish") || !strings.Contains(left, "goer") {
		t.Errorf("the tabs that were open are gone:\n%s", dump(g))
	}
	if !strings.Contains(left, "widgets") {
		t.Errorf("no tab was opened for the directory chosen:\n%s", dump(g))
	}
}

// rowFor is where a directory is offered in the list, which is never the query
// line however much the two look alike.
func rowFor(t *testing.T, snap func() *screen, want string) (x, y int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		g := snap()
		for row := 0; row < g.h; row++ {
			line := g.row(row)
			if strings.Contains(line, "›") {
				continue // the query line
			}
			if c := columnOf(line, want); c >= 0 {
				return c, row
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%q is not offered in the list:\n%s", want, dump(snap()))
	return 0, 0
}
