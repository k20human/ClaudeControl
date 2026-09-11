// Package clipboard reads the system clipboard.
//
// A terminal application cannot reach the clipboard on its own. There are two
// ways round that and neither always works, so both are tried: a helper
// program if one is installed, and otherwise OSC 52, which asks the terminal
// itself and is answered only by terminals that choose to.
package clipboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Selection is which of a desktop's two clipboards is meant.
//
// Linux has had both for as long as it has had windows: one you fill on
// purpose, and one that fills itself.
type Selection int

const (
	// System is the clipboard ctrl+c fills and ctrl+v empties.
	System Selection = iota

	// Primary is the one selecting text fills and a middle click empties. It
	// is filled without anybody asking, which is why it is a separate place:
	// dragging across a line must never cost you what you copied.
	Primary
)

// helpers are tried in order. The Wayland one first because that is what a
// current desktop runs; the X11 ones still answer under XWayland.
var helpers = map[Selection][][]string{
	System: {
		{"wl-paste", "--no-newline"},
		{"xclip", "-selection", "clipboard", "-o"},
		{"xsel", "--clipboard", "--output"},
	},
	Primary: {
		{"wl-paste", "--primary", "--no-newline"},
		{"xclip", "-selection", "primary", "-o"},
		{"xsel", "--primary", "--output"},
	},
}

// Helper names the program that would be used, and whether there is one. It
// exists so the interface can say what is missing rather than only that
// something failed.
func Helper() (string, bool) {
	for _, h := range helpers[System] {
		if _, err := exec.LookPath(h[0]); err == nil {
			return h[0], true
		}
	}
	return "", false
}

// Read returns the clipboard through a helper program.
//
// The timeout is not a formality: a helper on a session whose clipboard owner
// has gone away can wait indefinitely, and an interface must not.
func Read(timeout time.Duration) (string, error) {
	return ReadFrom(System, timeout)
}

// ReadFrom returns one of the two selections through a helper program.
func ReadFrom(sel Selection, timeout time.Duration) (string, error) {
	for _, h := range helpers[sel] {
		if _, err := exec.LookPath(h[0]); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		out, err := exec.CommandContext(ctx, h[0], h[1:]...).Output()
		cancel()
		if err != nil {
			return "", fmt.Errorf("clipboard: %s: %w", h[0], err)
		}
		return strings.TrimRight(string(out), "\n"), nil
	}
	return "", ErrNoHelper
}

// writers are the same programs, asked to take text rather than give it.
var writers = map[Selection][][]string{
	System: {
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
	},
	Primary: {
		{"wl-copy", "--primary"},
		{"xclip", "-selection", "primary"},
		{"xsel", "--primary", "--input"},
	},
}

// Write puts text on the clipboard through a helper program.
func Write(text string, timeout time.Duration) error {
	return WriteTo(System, text, timeout)
}

// WriteTo puts text on one of the two selections through a helper program.
func WriteTo(sel Selection, text string, timeout time.Duration) error {
	for i, w := range writers[sel] {
		if _, err := exec.LookPath(w[0]); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cmd := exec.CommandContext(ctx, w[0], w[1:]...)
		cmd.Stdin = strings.NewReader(text)
		err := cmd.Run()
		cancel()
		if err != nil {
			return fmt.Errorf("clipboard: %s: %w", w[0], err)
		}
		_ = i
		return nil
	}
	return ErrNoHelper
}

// ErrNoHelper says no clipboard program is installed. It is a distinct error
// because the remedy is distinct: one command to install one.
var ErrNoHelper = fmt.Errorf(
	"clipboard: none of wl-paste, xclip or xsel is installed")
