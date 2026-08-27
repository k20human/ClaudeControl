// Package alert tells you that something needs you when you are not looking.
//
// The indicator in a pane title only works if you are watching the screen, and
// the whole point of running several conversations is that you are not. Two
// ways out of the terminal, because neither is reliable on its own: the bell,
// which most terminals turn into an urgent mark on their tab and some ignore
// entirely, and the desktop notification, which needs a program that is not
// always installed.
package alert

import (
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"
)

// ErrNoHelper is returned when nothing on this machine can post a desktop
// notification. It is a fact about the machine, not a failure of the caller.
var ErrNoHelper = errors.New("alert: no notification program found")

// helpers are tried in order. notify-send is the freedesktop one and is what
// GNOME, KDE and the rest install; the others are what a machine without it
// tends to have instead.
var helpers = [][]string{
	{"notify-send", "--app-name=ClaudeControl", "--expire-time=8000"},
	{"kdialog", "--title", "ClaudeControl", "--passivepopup"},
}

// Options is what the configuration asked for.
type Options struct {
	// Bell writes BEL to the terminal. Free, and exactly what BEL is for.
	Bell bool
	// Desktop posts a notification through whatever program is installed.
	Desktop bool
}

// Notifier sends alerts. The zero value sends nothing, which is what a
// configuration that asked for nothing should get.
type Notifier struct {
	opts Options

	// term is written to under the lock: it arrives after construction, and
	// hooks can ring before it does.
	term io.Writer

	mu     sync.Mutex
	helper []string
	looked bool
}

// ring writes the bell, if there is somewhere to write it.
func (n *Notifier) ring() {
	n.mu.Lock()
	w := n.term
	n.mu.Unlock()
	if w != nil {
		_, _ = w.Write([]byte("\a"))
	}
}

// New returns a notifier writing its bell to term, which may be nil: the
// terminal is only opened once the application starts drawing, and hooks can
// arrive before that.
func New(opts Options, term io.Writer) *Notifier {
	return &Notifier{opts: opts, term: term}
}

// SetTerm gives the notifier somewhere to ring, once there is one.
func (n *Notifier) SetTerm(w io.Writer) {
	n.mu.Lock()
	n.term = w
	n.mu.Unlock()
}

// WantsDesktop reports whether the configuration asked for notifications.
func (n *Notifier) WantsDesktop() bool { return n != nil && n.opts.Desktop }

// Helper reports the program that will post notifications, and whether there
// is one. Asked before anything happens so that a configuration wanting
// notifications on a machine that cannot post them can be told at once,
// rather than at the moment one is missed.
func (n *Notifier) Helper() ([]string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.looked {
		return n.helper, n.helper != nil
	}
	n.looked = true
	for _, h := range helpers {
		if _, err := exec.LookPath(h[0]); err == nil {
			n.helper = h
			return n.helper, true
		}
	}
	return nil, false
}

// Waiting says that a conversation is waiting on you.
//
// The bell first and without waiting for anything: it is one byte and it is
// the part that works over ssh. The notification is posted in the background,
// because a helper that hangs must not hang the interface with it.
func (n *Notifier) Waiting(title, body string) error {
	if n == nil {
		return nil
	}
	if n.opts.Bell {
		n.ring()
	}
	if !n.opts.Desktop {
		return nil
	}
	helper, ok := n.Helper()
	if !ok {
		return ErrNoHelper
	}
	argv := append(append([]string{}, helper[1:]...), title, body)
	cmd := exec.Command(helper[0], argv...)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped in the background: an unwaited child is a zombie, and a session
	// that runs for a day would collect one an alert.
	go func() {
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	return nil
}
