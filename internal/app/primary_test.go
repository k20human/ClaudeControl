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

// fakePrimary puts stand-ins for both clipboard helpers first on the path: one
// that records what it was asked to take, and one that answers with a fixed
// text when the primary selection is asked for.
func fakePrimary(t *testing.T, answer string) (env []string, taken string) {
	t.Helper()
	dir := t.TempDir()
	taken = filepath.Join(dir, "taken")

	copyScript := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + taken + "\ncat >> " + taken + "\n"
	if err := os.WriteFile(filepath.Join(dir, "wl-copy"), []byte(copyScript), 0o755); err != nil {
		t.Fatal(err)
	}
	pasteScript := "#!/bin/sh\nif [ \"$1\" = --primary ]; then printf '%s' " +
		"'" + answer + "'; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "wl-paste"), []byte(pasteScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}, taken
}

// waitForFile polls until a file holds what is wanted. The hand-off to the
// desktop happens off the draw loop, so there is nothing on screen to wait for.
func waitForFile(t *testing.T, path, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), want) {
			return string(b)
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("%q never reached the desktop; the helper was given:\n%s", want, b)
	return ""
}

// Selecting with the mouse hands the text to the desktop's primary selection,
// the way it does in every other window. Nothing is pressed to make it happen:
// that is what makes it the primary selection rather than the clipboard.
func TestADragHandsTheTextToThePrimarySelection(t *testing.T) {
	const W, H = 60, 10
	env, taken := fakePrimary(t, "")
	s, snap := runWithEnv(t, selectConfig(t, `"printf 'HELLO-WORLD'; cat"`), W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "HELLO-WORLD")

	row := paneRow0
	from := columnOf(snap().row(row), "HELLO")
	if from < 0 {
		t.Fatalf("no text to select: %q", snap().row(row))
	}
	click(t, s, 5, row)
	press(t, s, from, row)
	drag(t, s, from+4, row)
	release(t, s, from+4, row)

	got := waitForFile(t, taken, "HELLO")
	// The primary selection and the clipboard are two places. Handing a drag
	// to the clipboard would cost you what you had copied, every time.
	if !strings.HasPrefix(got, "--primary") {
		t.Errorf("the helper was called as %q, want the primary selection", strings.SplitN(got, "\n", 2)[0])
	}
}

// The middle button pastes it back, into the pane it was clicked in.
func TestTheMiddleButtonPastesTheSelection(t *testing.T) {
	const W, H = 60, 10
	const fromDesktop = "COLLE-DU-BUREAU"
	env, _ := fakePrimary(t, fromDesktop)
	s, snap := runWithEnv(t, selectConfig(t, `"printf 'READY'; cat"`), W, H, vt.Callbacks{}, env)
	waitForAnywhere(t, snap, "READY")

	click(t, s, 5, paneRow0) // focus
	middleClick(t, s, 10, paneRow0)

	// The guest echoes what it is sent, so the text appearing is the paste
	// having arrived — text selected in another window, not in this one.
	waitForAnywhere(t, snap, fromDesktop)
}

// middleClick presses and releases the middle button, which a terminal reports
// as button 1.
func middleClick(t *testing.T, s sender, x, y int) {
	t.Helper()
	s.SendText(fmt.Sprintf("\x1b[<1;%d;%dM", x+1, y+1))
	time.Sleep(80 * time.Millisecond)
	s.SendText(fmt.Sprintf("\x1b[<1;%d;%dm", x+1, y+1))
	time.Sleep(150 * time.Millisecond)
}

// With no helper installed the desktop cannot be asked, and the gesture still
// works inside this window: what was selected here is remembered here.
func TestTheMiddleButtonWorksWithNoHelperInstalled(t *testing.T) {
	const W, H = 60, 10
	s, snap := runWithEnv(t, selectConfig(t, `"printf 'HELLO-WORLD'; cat"`), W, H,
		vt.Callbacks{}, noHelperPath(t))
	waitForAnywhere(t, snap, "HELLO-WORLD")

	row := paneRow0
	from := columnOf(snap().row(row), "HELLO")
	if from < 0 {
		t.Fatalf("no text to select: %q", snap().row(row))
	}
	click(t, s, 5, row)
	press(t, s, from, row)
	drag(t, s, from+4, row)
	release(t, s, from+4, row)

	middleClick(t, s, 10, row+2)

	// The guest echoes what it is sent, and what it was sent is the selection
	// — so the word appears a second time, on the row the guest is writing on.
	waitFor(t, snap, func(g *screen) bool {
		seen := 0
		for y := 0; y < H; y++ {
			seen += strings.Count(g.row(y), "HELLO")
		}
		return seen >= 2
	}, "the selection pasted back into the pane")
}

// noHelperPath is a path with a shell and the one program the guest needs, and
// no clipboard helper at all — the machine where the desktop cannot be asked.
func noHelperPath(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"sh", "cat"} {
		found, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("no %s to link: %v", name, err)
		}
		if err := os.Symlink(found, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return []string{"PATH=" + dir}
}
