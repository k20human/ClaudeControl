package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// transcriptFixture writes a small corpus of conversations and returns the
// configuration directory that holds them, ready to be given to the hosted
// application as CLAUDE_CONFIG_DIR.
func transcriptFixture(t *testing.T) (configDir, workDir string) {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "rocketwork")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	projects := filepath.Join(root, "config", "projects", "-rocketwork")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}

	// One conversation that says the word, and one that only has it in a tool
	// result — which is the machine talking to itself and must not match.
	said := strings.Join([]string{
		`{"type":"user","aiTitle":"Repulsor tuning","cwd":"` + work + `","message":{"role":"user","content":"how do I balance the repulsorlift?"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Trim the repulsorlift by hand first."}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(projects, "aaaa1111-2222-3333-4444-555566667777.jsonl"), []byte(said), 0o600); err != nil {
		t.Fatal(err)
	}

	quoted := `{"type":"user","aiTitle":"Log reading","cwd":"` + work +
		`","message":{"role":"user","content":[{"type":"tool_result","content":"repulsorlift: ok"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, "bbbb1111-2222-3333-4444-555566667777.jsonl"), []byte(quoted), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "config"), work
}

// The search the bar offers looks through the conversations on disk, not the
// pane in front of you: what you remember saying is rarely on the screen.
func TestTheConversationSearchFindsWhatWasSaid(t *testing.T) {
	const W, H = 120, 24
	cfgDir, _ := transcriptFixture(t)
	s, snap := runWithEnv(t, tabsConfig(t), W, H, vt.Callbacks{}, []string{"CLAUDE_CONFIG_DIR=" + cfgDir})
	waitForAnywhere(t, snap, "ALPHA-HERE")

	bar := waitForRow(t, snap, H-1, "find").row(H - 1)
	click(t, s, columnOf(bar, "find"), H-1)
	waitForAnywhere(t, snap, "find in conversations")

	s.SendText("repulsorlift")
	waitForAnywhere(t, snap, "Repulsor tuning")

	g := snap()
	if anywhere(g, "Log reading") {
		t.Error("a tool result matched; only what was said should")
	}
	if !anywhere(g, "repulsorlift") {
		t.Error("the result carries no excerpt of what was said")
	}
}

// Opening a result puts the conversation in a tab beside what you were already
// reading, rather than taking the pane away from you.
func TestOpeningAResultAddsATab(t *testing.T) {
	const W, H = 120, 24
	cfgDir, _ := transcriptFixture(t)
	s, snap := runWithEnv(t, tabsConfig(t), W, H, vt.Callbacks{}, []string{"CLAUDE_CONFIG_DIR=" + cfgDir})
	waitForAnywhere(t, snap, "ALPHA-HERE")

	// The right-hand pane is the tabbed one; the search opens into whichever
	// pane has the focus.
	click(t, s, 3*W/4, 5)

	bar := waitForRow(t, snap, H-1, "find").row(H - 1)
	click(t, s, columnOf(bar, "find"), H-1)
	waitForAnywhere(t, snap, "find in conversations")
	s.SendText("repulsorlift")
	waitForAnywhere(t, snap, "Repulsor tuning")

	s.SendText("\r")
	// The tab is named after the directory the conversation ran in, which is
	// how you tell two of them apart.
	waitForAnywhere(t, snap, "rocketwork")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !anywhere(snap(), "find in conversations") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if anywhere(snap(), "find in conversations") {
		t.Error("the panel stayed open after opening a result")
	}
	if !strings.Contains(snap().row(0), "alpha") {
		t.Errorf("the tabs that were there are gone: %q", snap().row(0))
	}
}
