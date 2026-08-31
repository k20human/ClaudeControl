package claude_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/module"
)

// grid is a screen a test can read back.
type grid struct {
	w, h  int
	cells []uv.Cell
}

func newGrid(w, h int) *grid {
	g := &grid{w: w, h: h, cells: make([]uv.Cell, w*h)}
	for i := range g.cells {
		g.cells[i] = uv.EmptyCell
	}
	return g
}

func (g *grid) Bounds() uv.Rectangle { return uv.Rect(0, 0, g.w, g.h) }
func (g *grid) CellAt(x, y int) *uv.Cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return nil
	}
	return &g.cells[y*g.w+x]
}
func (g *grid) SetCell(x, y int, c *uv.Cell) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h || c == nil {
		return
	}
	g.cells[y*g.w+x] = *c
}
func (g *grid) WidthMethod() uv.WidthMethod { return ansi.GraphemeWidth }
func (g *grid) text() string {
	var b strings.Builder
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			b.WriteString(g.cells[y*g.w+x].Content)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// stubClaude is a claude that says something and ends, which is the whole of
// what these tests need from one.
func stubClaude(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// A conversation that ends leaves the pane at a prompt, in the directory it
// was running in. It used to leave a dead screen with nothing to be done.
func TestAFinishedConversationLeavesAShellBehind(t *testing.T) {
	dir := t.TempDir()
	m, err := module.New("claude", map[string]any{
		"dir": dir,
		"bin": stubClaude(t, "printf 'CONVERSATION-OVER\\n'\nexit 0\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Init(module.Context{Wake: func() {}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Resize(70, 10); err != nil {
		t.Fatal(err)
	}

	var out string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		g := newGrid(70, 10)
		m.Draw(g, uv.Rect(0, 0, 70, 10))
		out = g.text()
		if strings.Contains(out, "conversation ended") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(out, "conversation ended") {
		t.Fatalf("nothing took over from the conversation:\n%s", out)
	}
	// And it says what to do next, which is the whole complaint: a pane you
	// can do nothing with says nothing about why.
	if !strings.Contains(out, "alt+a") {
		t.Errorf("the pane does not say how to open another session:\n%s", out)
	}

	// The shell is a shell: it answers.
	in, ok := m.(interface{ Paste(string) })
	if !ok {
		t.Fatal("the pane takes no input")
	}
	in.Paste("printf SHELL-ALIVE\n")
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		g := newGrid(70, 10)
		m.Draw(g, uv.Rect(0, 0, 70, 10))
		if strings.Contains(g.text(), "SHELL-ALIVE") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	g := newGrid(70, 10)
	m.Draw(g, uv.Rect(0, 0, 70, 10))
	t.Errorf("the shell did not answer:\n%s", g.text())
}

// A conversation that is still running is left alone.
func TestALiveConversationIsNotReplaced(t *testing.T) {
	m, err := module.New("claude", map[string]any{
		"dir": t.TempDir(),
		"bin": stubClaude(t, "printf 'STILL-HERE\\n'\nsleep 30\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Init(module.Context{Wake: func() {}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Resize(70, 10); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		g := newGrid(70, 10)
		m.Draw(g, uv.Rect(0, 0, 70, 10))
		if strings.Contains(g.text(), "STILL-HERE") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	g := newGrid(70, 10)
	m.Draw(g, uv.Rect(0, 0, 70, 10))
	if strings.Contains(g.text(), "conversation ended") {
		t.Errorf("a conversation that is still running was replaced:\n%s", g.text())
	}
}

// Ending a conversation is a decision. Bringing it back on the next run
// because the pane is still open would undo that decision for you.
func TestAFinishedConversationIsNotBroughtBack(t *testing.T) {
	m, err := module.New("claude", map[string]any{
		"dir": t.TempDir(),
		"bin": stubClaude(t, "printf 'DONE\\n'\nexit 0\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Init(module.Context{Wake: func() {}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.Resize(70, 10); err != nil {
		t.Fatal(err)
	}

	lister := m.(interface{ Sessions() []string })
	if len(lister.Sessions()) != 1 {
		t.Fatalf("a live conversation reports %v, want one", lister.Sessions())
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		g := newGrid(70, 10)
		m.Draw(g, uv.Rect(0, 0, 70, 10))
		if strings.Contains(g.text(), "conversation ended") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := lister.Sessions(); len(got) != 0 {
		t.Errorf("a finished conversation is still offered for tomorrow: %v", got)
	}
}
