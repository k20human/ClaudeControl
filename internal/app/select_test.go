package app_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// press, moveTo and release drive a drag the way a terminal reports one.
func press(t *testing.T, s sender, x, y int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1))
	time.Sleep(80 * time.Millisecond)
}

// fakeClipboard puts a stand-in wl-copy first on the PATH, so a test never
// writes to the clipboard of whoever is running it. It returns the
// environment to hand the application and the file the stand-in writes to.
func fakeClipboard(t *testing.T) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "clipboard")
	script := "#!/bin/sh\ncat > " + out + "\n"
	if err := os.WriteFile(filepath.Join(dir, "wl-copy"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir + ":" + os.Getenv("PATH")}, out
}

// noClipboard puts a PATH with no clipboard program on it at all.
//
// It cannot simply drop a directory: wl-copy lives in /usr/bin beside the
// shell the panes need. So the directory holds a link to the shell and
// nothing else.
func noClipboard(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("no shell to link: %v", err)
	}
	if err := os.Symlink(sh, filepath.Join(dir, "sh")); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir}
}

func selectConfig(t *testing.T, script string) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "s.yaml")
	if err := os.WriteFile(cfg, []byte("layout:\n  module: term\n  options:\n    cmd: [sh, -c, "+script+"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// reversedAt reports whether a cell is drawn inverted, which is how a
// selection is shown.
func reversedAt(g *screen, x, y int) bool {
	return reversed(g, x, y)
}

// A guest that asked only for button events cannot see a drag: it arrives as a
// press and a release with nothing in between. The gesture is therefore free,
// and it is what a person expects to select text with.
func TestDraggingSelectsTextAGuestCannotUse(t *testing.T) {
	const W, H = 60, 10
	s, snap := run(t, selectConfig(t, `"printf 'HELLO-WORLD'; printf '\x1b[?1000h\x1b[?1006h'; cat -v"`), W, H)
	waitForAnywhere(t, snap, "HELLO-WORLD")

	row := paneRow0
	from := columnOf(snap().row(row), "HELLO")
	if from < 0 {
		t.Fatalf("no text to select: %q", snap().row(row))
	}

	click(t, s, 5, row) // focus first
	press(t, s, from, row)
	drag(t, s, from+4, row)
	release(t, s, from+4, row)

	g := snap()
	for x := from; x <= from+4; x++ {
		if !reversedAt(g, x, row) {
			t.Errorf("column %d is not marked as selected: %q", x, g.row(row))
		}
	}
	if reversedAt(g, from+6, row) {
		t.Errorf("the selection ran past where the drag ended: %q", g.row(row))
	}
}

// The guest still gets its clicks: a press that never moves is a click, and a
// guest that asked for button events is entitled to it.
func TestAClickStillReachesTheGuest(t *testing.T) {
	const W, H = 60, 10
	s, snap := run(t, selectConfig(t, `"printf 'READY'; printf '\x1b[?1000h\x1b[?1006h'; cat -v"`), W, H)
	waitForAnywhere(t, snap, "READY")

	click(t, s, 5, paneRow0)  // focus
	click(t, s, 10, paneRow0) // and a real click, which the guest echoes
	waitForAnywhere(t, snap, "[<0;11;")
}

// A guest that does follow the pointer keeps its drag: it is doing something
// with it, and taking the gesture would break whatever that is.
func TestAGuestThatFollowsThePointerKeepsItsDrag(t *testing.T) {
	const W, H = 60, 10
	s, snap := run(t, selectConfig(t, `"printf 'READY'; printf '\x1b[?1002h\x1b[?1006h'; cat -v"`), W, H)
	waitForAnywhere(t, snap, "READY")

	click(t, s, 5, paneRow0)
	press(t, s, 10, paneRow0)
	drag(t, s, 20, paneRow0)
	release(t, s, 20, paneRow0)

	// The motion reached it, and nothing was selected here.
	waitForAnywhere(t, snap, "[<32;21;")
	g := snap()
	for x := 10; x <= 20; x++ {
		if reversedAt(g, x, paneRow0) {
			t.Fatalf("a pane whose guest owns the drag showed a selection: %q", g.row(paneRow0))
		}
	}
}

// Copying without a selection says so rather than doing nothing.
func TestCopyingNothingSaysSo(t *testing.T) {
	const W, H = 60, 10
	s, snap := run(t, selectConfig(t, `"printf 'READY'; cat"`), W, H)
	waitForAnywhere(t, snap, "READY")

	rightClick(t, s, 10, paneRow0)
	waitForAnywhere(t, snap, "copy")

	row := snap().row(0)
	at := columnOf(row, "copy")
	if at < 0 {
		// The menu may be drawn lower; find it wherever it is.
		for y := 0; y < H; y++ {
			if c := columnOf(snap().row(y), "copy"); c >= 0 {
				click(t, s, c, y)
				at = c
				break
			}
		}
	} else {
		click(t, s, at, 0)
	}
	if at < 0 {
		t.Fatal("no copy entry in the menu")
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(snap().row(H-1), "nothing is selected") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("copying nothing said nothing: %q", snap().row(H-1))
}

// A copy that reached nothing must not read like a copy that worked. Without a
// clipboard tool the terminal is asked directly, and most refuse — so the menu
// says what it will need before you press it, and the message afterwards leads
// with the remedy, since what a truncated line cuts has to be the part you
// could have guessed.
func TestCopyWithoutAClipboardToolSaysSoBeforeAndAfter(t *testing.T) {
	const W, H = 70, 10
	s, snap := runWithEnv(t, selectConfig(t, `"printf 'HELLO-WORLD'; cat"`), W, H,
		vt.Callbacks{}, noClipboard(t))
	waitForAnywhere(t, snap, "HELLO-WORLD")

	click(t, s, 5, paneRow0)
	rightClick(t, s, 10, paneRow0)
	waitForAnywhere(t, snap, "copy")
	if !anywhere(snap(), "no clipboard tool") {
		t.Errorf("the menu does not warn before the attempt:\n%s", snap().row(0))
	}
}

// With one, the copy happens and says how much.
func TestCopyWithAClipboardToolCopies(t *testing.T) {
	const W, H = 70, 10
	env, out := fakeClipboard(t)
	s, snap := runWithEnv(t, selectConfig(t, `"printf 'HELLO-WORLD'; cat"`), W, H,
		vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "HELLO-WORLD")

	row := paneRow0
	from := columnOf(snap().row(row), "HELLO")
	click(t, s, 5, row)
	press(t, s, from, row)
	drag(t, s, from+4, row)
	release(t, s, from+4, row)

	rightClick(t, s, 10, row)
	waitForAnywhere(t, snap, "copy")
	if anywhere(snap(), "no clipboard tool") {
		t.Errorf("the menu warns although a tool is present:\n%s", snap().row(0))
	}

	at, atY := -1, -1
	for y := 0; y < H; y++ {
		if c := columnOf(snap().row(y), "copy"); c >= 0 {
			at, atY = c, y
			break
		}
	}
	if at < 0 {
		t.Fatal("no copy entry in the menu")
	}
	click(t, s, at, atY)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(out); err == nil && strings.Contains(string(raw), "HELLO") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	raw, _ := os.ReadFile(out)
	t.Errorf("the clipboard received %q; the bar says %q", string(raw), snap().row(H-1))
}

// The menu is three gestures away from a selection you have already made.
// alt+c is one, and it copies the same text the menu would.
//
// The key had to pass the same three tests as every other: absent from the
// Claude Code binary, intact through a hosting emulator, and cheap to give up
// in a shell — it shadows readline's capitalize-word and nothing else.
func TestTheCopyShortcutCopies(t *testing.T) {
	const W, H = 70, 10
	env, out := fakeClipboard(t)
	s, snap := runWithEnv(t, selectConfig(t, `"printf 'SHORTCUT-HERE'; cat"`), W, H,
		vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "SHORTCUT-HERE")

	row := paneRow0
	from := columnOf(snap().row(row), "SHORTCUT")
	click(t, s, 5, row)
	press(t, s, from, row)
	drag(t, s, from+7, row)
	release(t, s, from+7, row)

	s.SendText("\x1bc") // alt+c

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(out); err == nil && strings.Contains(string(raw), "SHORTCUT") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	raw, _ := os.ReadFile(out)
	t.Errorf("the clipboard received %q; the bar says %q", string(raw), snap().row(H-1))
}

// A pane of tabs stands between the application and what it holds, and the
// selected text has to reach across it. It did not: copy answered "nothing in
// this pane can be selected", which was true of the pane and false of the tab
// in it.
func TestCopyingWorksInsideATabbedPane(t *testing.T) {
	const W, H = 80, 12
	env, out := fakeClipboard(t)
	cfg := filepath.Join(t.TempDir(), "t.yaml")
	if err := os.WriteFile(cfg, []byte(`layout:
  module: tabs
  options:
    tabs:
      - { title: one, module: term, options: { cmd: [sh, -c, "printf 'INSIDE-A-TAB'; cat"] } }
      - { title: two, module: term, options: { cmd: [sh, -c, "cat"] } }
`), 0o600); err != nil {
		t.Fatal(err)
	}

	s, snap := runWithEnv(t, cfg, W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "INSIDE-A-TAB")

	row := paneRow0
	from := columnOf(snap().row(row), "INSIDE")
	if from < 0 {
		t.Fatalf("no text to select: %q", snap().row(row))
	}
	click(t, s, 5, row)
	press(t, s, from, row)
	drag(t, s, from+5, row)
	release(t, s, from+5, row)

	rightClick(t, s, 10, row)
	waitForAnywhere(t, snap, "copy")
	at, atY := -1, -1
	for y := 0; y < H; y++ {
		if c := columnOf(snap().row(y), "copy"); c >= 0 {
			at, atY = c, y
			break
		}
	}
	if at < 0 {
		t.Fatal("no copy entry in the menu")
	}
	click(t, s, at, atY)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(out); err == nil && strings.Contains(string(raw), "INSIDE") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	raw, _ := os.ReadFile(out)
	t.Errorf("the clipboard received %q; the bar says %q", string(raw), snap().row(H-1))
}
