package claude

import (
	"fmt"
	"os"
	"strings"

	"claudecontrol/internal/session"
)

// What happens when a conversation ends.
//
// Nothing used to. The pane kept the dead screen, and in a pane of tabs it did
// not even carry the banner saying so — you were left looking at something you
// could neither use nor understand. Now the pane goes back to being what it
// was before Claude was started in it: a shell, in the same directory, with a
// line saying what became of the conversation.
//
// A shell rather than another conversation because ending one is a decision,
// and starting the next is a different decision. The line says which key makes
// that one.

// carryOn replaces a finished conversation with a shell, once.
//
// Called from Draw, which is the only thing that happens regularly enough to
// notice. It is guarded and idempotent, and the alternative — a ticker in
// every pane holding a conversation — would cost more than it saves.
func (m *Module) carryOn() {
	if m.sess == nil || m.shell {
		return
	}
	st, code := m.sess.Status()
	if st != session.Exited {
		return
	}

	w, h := m.sess.Term.Bounds().Dx(), m.sess.Term.Bounds().Dy()
	if w < 1 || h < 1 {
		w, h = 80, 24
	}
	old := m.sess
	m.shell = true

	s, err := session.Start(session.Spec{
		ID:       session.ID(m.SessionID() + "-shell"),
		Argv:     shellArgv(),
		Dir:      m.dir,
		Width:    w,
		Height:   h,
		OnUpdate: m.ctx.Wake,
	})
	if err != nil {
		// No shell to be had. The dead screen stays, which is no worse than
		// it was, and the banner the application draws still explains it.
		m.shell = false
		return
	}

	// Said in the pane rather than in the status bar: this is about this pane,
	// and the bar would have moved on by the time you looked.
	s.Feed([]byte(fmt.Sprintf(
		"\x1b[2m— conversation ended (%d) · shell in %s · alt+a opens another session —\x1b[0m\r\n",
		code, m.dir)))

	m.sess = s
	m.view.Reset()
	if m.ctx.Pool != nil {
		// The conversation is over: it leaves the list, and the shell does not
		// join it. A shell is not something to bring back.
		_ = m.ctx.Pool.Kill(old.ID)
	} else {
		_ = old.Close()
	}
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

// shellArgv is the shell to fall back on: the one you have chosen, interactive
// so that it gives you a prompt.
func shellArgv() []string {
	sh := os.Getenv("SHELL")
	if strings.TrimSpace(sh) == "" {
		sh = "/bin/sh"
	}
	return []string{sh, "-i"}
}
