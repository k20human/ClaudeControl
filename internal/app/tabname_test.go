package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// fakeClaude stands in for Claude Code: it writes a transcript that names the
// conversation, then fires the hook the way Claude Code does — by running the
// command out of the --settings it was handed, with a payload on stdin.
//
// Faithful to the one thing this test is about: the identity. The name reaches
// the application under whatever identity the hook carries, and the tab looks
// for the identity its own module reports. Nothing proves those are the same
// value but a session that really starts and a hook that really fires.
func fakeClaude(t *testing.T, into, title string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := `#!/usr/bin/env python3
import json, os, subprocess, sys

argv = sys.argv[1:]

def val(flag):
    return argv[argv.index(flag) + 1] if flag in argv else ''

# The identity the application imposed, or the one it asked to resume. Either
# way it is what the hook will carry, and what names the transcript here.
ident = val('--session-id') or val('--resume') or 'unknown'
hooks = json.loads(val('--settings'))['hooks']

# Where Claude Code keeps transcripts, when a test has said where that is:
# a conversation is resumable only if its transcript can be found by id.
config = os.environ.get('CLAUDE_CONFIG_DIR', '')
folder = os.path.join(config, 'projects', 'testproject') if config else ` + quote(into) + `
os.makedirs(folder, exist_ok=True)

transcript = os.path.join(folder, ident + '.jsonl')
with open(transcript, 'w') as f:
    f.write(json.dumps({
        'type': 'ai-title',
        'sessionId': ident,
        'aiTitle': ` + quote(title) + `,
    }) + '\n')

# The hook that fires when a turn ends, which is the one that carries the
# transcript. Only that one: a session reported as working animates the window
# title, and a title rewritten every frame is a title being written over the
# screen a test is reading.
payload = json.dumps({
    'session_id': ident,
    'transcript_path': transcript,
    'hook_event_name': 'Stop',
})
subprocess.run(hooks['Stop'][0]['hooks'][0]['command'], shell=True, input=payload.encode())

sys.stdout.write('FAKE-CLAUDE\n')
sys.stdout.flush()
for _ in sys.stdin:
    pass
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// quote writes a Python string literal.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "\\'") + "'" }

// The whole chain, from a hook firing to the strip: Claude Code names a
// conversation, the name reaches the application under the pane's identity,
// and the tab holding that conversation wears it.
//
// The two halves of this were tested apart — the application publishes a name,
// a strip takes one it is given — and a chain tested only in halves is a chain
// with an untested seam. The seam is the identity: the hook's and the tab's
// have to be the same string.
func TestATabWearsTheNameARealHookBrings(t *testing.T) {
	const W, H = 100, 14
	const named = "Analyse Wayland"

	work := filepath.Join(t.TempDir(), "DEV")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := fakeClaude(t, t.TempDir(), named)

	cfg := filepath.Join(t.TempDir(), "tabbed.yaml")
	body := `layout:
  module: tabs
  options:
    tabs:
      - module: claude
        options:
          bin: ` + bin + `
          dir: ` + work + `
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, snap := run(t, cfg, W, H)
	waitForAnywhere(t, snap, "FAKE-CLAUDE")
	waitForAnywhere(t, snap, named)
}

// The same chain for a tab opened with alt+a, which is how a tab is really
// opened: nothing on the command line, a session in the directory the pane
// works in, and a title taken from that directory — "DEV" — until Claude Code
// says what the conversation is about.
//
// The tab in the file and the tab from alt+a are not built the same way, and
// only one of them was covered.
func TestATabOpenedWithAltAWearsTheName(t *testing.T) {
	const W, H = 100, 14
	const named = "Analyse Wayland"

	work := filepath.Join(t.TempDir(), "DEV")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	// alt+a runs whatever "claude" means on the path, so the stand-in has to
	// be called that and has to come first.
	bin := fakeClaude(t, t.TempDir(), named)
	onPath := filepath.Dir(bin)

	cfg := filepath.Join(t.TempDir(), "shell.yaml")
	body := `layout:
  module: tabs
  options:
    dir: ` + work + `
    tabs:
      - title: shell
        module: term
        options: { cmd: [sh, -c, "printf SHELL-HERE; cat"] }
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	s, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, []string{
		"PATH=" + onPath + string(os.PathListSeparator) + os.Getenv("PATH"),
	})
	waitForAnywhere(t, snap, "SHELL-HERE")

	s.SendText("\x1ba") // alt+a
	waitForAnywhere(t, snap, "FAKE-CLAUDE")

	// It starts named after its directory, and takes the conversation's name.
	waitForAnywhere(t, snap, named)
}

// A name you type is kept, and comes back with the conversation it names.
//
// Both halves matter, and only together: a name that Claude Code overwrites on
// the next turn is not a name you gave, and a name that does not survive
// closing the window is not one you kept.
func TestANameYouGiveATabComesBackWithIt(t *testing.T) {
	const W, H = 100, 14
	const published = "Analyse Wayland"
	const mine = "facturation"

	state := t.TempDir()     // what the two runs share
	claudeCfg := t.TempDir() // where the transcripts go
	work := filepath.Join(t.TempDir(), "DEV")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	bin := fakeClaude(t, t.TempDir(), published)

	cfg := filepath.Join(t.TempDir(), "tabbed.yaml")
	body := `layout:
  module: tabs
  options:
    tabs:
      - module: claude
        options:
          bin: ` + bin + `
          dir: ` + work + `
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"XDG_STATE_HOME=" + state, "CLAUDE_CONFIG_DIR=" + claudeCfg}

	s, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "FAKE-CLAUDE")
	waitForAnywhere(t, snap, published)

	// F2, a name, enter.
	s.SendText("\x1bOQ")
	waitForAnywhere(t, snap, "name:")
	s.SendText(mine + "\r")

	// Waiting for the name to appear is not enough: the prompt echoes what
	// you type, so the name is on the screen before enter has been read. What
	// says the rename happened is the prompt being gone.
	waitFor(t, snap, func(g *screen) bool {
		row := g.row(0)
		return strings.Contains(row, mine) && !strings.Contains(row, "name:")
	}, "the tab to wear the name")

	// The name it was given stays given: Claude Code renames a conversation
	// as it goes, and the next turn must not take the tab back.
	if row := snap().row(0); strings.Contains(row, published) {
		t.Fatalf("the published name came back over yours: %q", row)
	}

	// And it is written down before the run ends, not on the way out: a name
	// is worth keeping even when a window is closed the hard way.
	waitForName(t, filepath.Join(state, "claudecontrol", "state.json"), mine)
	_ = s.Close()

	// A second run, with the same state and the same conversation to resume.
	s2, snap2 := runWithEnv(t, cfg, W, H, vt.Callbacks{}, env)
	_ = s2
	waitForAnywhere(t, snap2, "FAKE-CLAUDE")
	waitFor(t, snap2, func(g *screen) bool {
		return strings.Contains(g.row(0), mine)
	}, "the name you gave, back where you left it")
	if row := snap2().row(0); strings.Contains(row, published) {
		t.Errorf("the new run named the tab itself: %q", row)
	}
}

// waitForName polls the snapshot file until it holds a name. The file is
// written by the application, so a test that read it once could read it
// before it was there.
func waitForName(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("%q was never written down; the snapshot reads:\n%s", want, b)
}

// muteClaude is a session that says nothing: no hook, no transcript, just a
// pane that stays open. What a conversation looks like when you bring it back
// and do not speak to it.
func muteClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf 'MUTE-CLAUDE\\n'\ncat\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// A conversation brought back from the last run is named straight away.
//
// Nothing followed its transcript until a hook said where it was, and no hook
// fires until you speak to a session — so every restored tab wore the name of
// its directory until you typed in it, with the name sitting in a file on disk
// the whole time. This session fires no hook at all, so the name can only come
// from the transcript being read at startup.
func TestAConversationBroughtBackIsNamedWithoutWaitingForAHook(t *testing.T) {
	const W, H = 100, 14
	const published = "Analyse Wayland"

	state := t.TempDir()
	claudeCfg := t.TempDir()
	work := filepath.Join(t.TempDir(), "DEV")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{"XDG_STATE_HOME=" + state, "CLAUDE_CONFIG_DIR=" + claudeCfg}

	// A first run, which has the conversation and hears its name from a hook.
	first := tabbedConfig(t, fakeClaude(t, t.TempDir(), published), work)
	s, snap := runWithEnv(t, first, W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap, published)
	_ = s.Close()

	// A second run, whose session reports nothing at all.
	second := tabbedConfig(t, muteClaude(t), work)
	_, snap2 := runWithEnv(t, second, W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap2, "MUTE-CLAUDE")
	waitFor(t, snap2, func(g *screen) bool {
		return strings.Contains(g.row(0), published)
	}, "the name of a conversation nobody has spoken to yet")
}

// tabbedConfig is a pane of tabs holding one conversation.
func tabbedConfig(t *testing.T, bin, dir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tabbed.yaml")
	body := `layout:
  module: tabs
  options:
    tabs:
      - module: claude
        options:
          bin: ` + bin + `
          dir: ` + dir + `
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A double-click on a label names it, the way it does on the tabs of an
// editor: the gesture nobody has to be told about.
func TestADoubleClickOnATabNamesIt(t *testing.T) {
	const W, H = 100, 14
	work := filepath.Join(t.TempDir(), "DEV")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := tabbedConfig(t, muteClaude(t), work)
	s, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, []string{
		"CLAUDE_CONFIG_DIR=" + t.TempDir(),
	})
	waitForAnywhere(t, snap, "MUTE-CLAUDE")

	// On the label, twice.
	x := columnOf(snap().row(0), "claude")
	if x < 0 {
		t.Fatalf("no label to click: %q", snap().row(0))
	}
	click(t, s, x, 0)
	click(t, s, x, 0)
	waitForAnywhere(t, snap, "name:")

	s.SendText("facturation\r")
	waitFor(t, snap, func(g *screen) bool {
		row := g.row(0)
		return strings.Contains(row, "facturation") && !strings.Contains(row, "name:")
	}, "the name to take")
}
