package app

import (
	"fmt"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/session"
)

// Closing something that holds a conversation.
//
// Closing a pane is a statement about the screen: the conversation it held
// keeps running, and is reachable from alt+space. That is the right answer
// when you meant to make room, and the wrong one when you meant to be done —
// and nothing in the gesture says which. Three conversations left running that
// way, in one afternoon, were the evidence that it cannot be guessed.
//
// So it asks, and only where there is something to lose sight of: a shell ends
// either way, a conversation that has already ended has nothing left to keep,
// and a question about nothing is a question nobody reads.

// closing is the tab or pane waiting on that answer.
type closing struct {
	pane layout.PaneID
	// index is the tab, or -1 for the pane itself.
	index int
	title string
	held  []string
}

// conversationsIn are the conversations a module would want back tomorrow.
//
// Sessioner rather than an identity: a shell has a process and an id like
// anything else, and neither is something to think twice about closing.
func conversationsIn(m module.Module) []string {
	lister, ok := m.(module.Sessioner)
	if !ok {
		return nil
	}
	return lister.Sessions()
}

// askCloseTab closes a tab, or asks first when the tab holds a conversation.
func (a *App) askCloseTab(id layout.PaneID, i int) {
	t, ok := a.paneNamer(id)
	if !ok {
		return
	}
	held := a.tabConversations(id, i)
	if len(held) == 0 {
		a.closeTabNow(id, i)
		return
	}
	a.closing = &closing{pane: id, index: i, title: t.TitleAt(i), held: held}
	a.overlay = overlayClosing
	a.Wake()
}

// askClosePane is the same question for a pane that has no tabs to close.
func (a *App) askClosePane(id layout.PaneID) {
	m, ok := a.modules[id]
	if !ok {
		return
	}
	held := conversationsIn(m)
	if len(held) == 0 {
		_ = a.closePane(id)
		return
	}
	a.closing = &closing{pane: id, index: -1, title: a.paneName(id), held: held}
	a.overlay = overlayClosing
	a.Wake()
}

// tabConversations are the conversations one tab holds.
func (a *App) tabConversations(id layout.PaneID, i int) []string {
	m, ok := a.modules[id]
	if !ok {
		return nil
	}
	lister, ok := m.(interface{ SessionsAt(int) []string })
	if !ok {
		return nil
	}
	return lister.SessionsAt(i)
}

// finishClose acts on the answer: the tab or pane goes either way, and end
// says whether the conversation goes with it.
func (a *App) finishClose(end bool) {
	c := a.closing
	a.closing = nil
	a.overlay = overlayNone
	a.clearNext = true
	if c == nil {
		return
	}
	if end && a.pool != nil {
		// Ended before the pane lets go of it. The other order would detach
		// it first, and what is detached is what nothing is holding.
		for _, id := range c.held {
			_ = a.pool.Kill(session.ID(a.liveSession(id)))
			_ = a.pool.Kill(session.ID(id))
		}
	}
	if c.index < 0 {
		_ = a.closePane(c.pane)
		return
	}
	a.closeTabNow(c.pane, c.index)
}

// cancelClose leaves everything as it was.
func (a *App) cancelClose() {
	a.closing = nil
	a.overlay = overlayNone
	a.clearNext = true
	a.Wake()
}

// closeTabNow closes a tab, and the pane with it when it was the last one.
func (a *App) closeTabNow(id layout.PaneID, i int) {
	m, ok := a.modules[id]
	if !ok {
		return
	}
	t, ok := m.(interface {
		CloseTab(int) error
		Count() int
	})
	if !ok {
		return
	}
	if t.Count() <= 1 {
		_ = a.closePane(id)
		return
	}
	if err := t.CloseTab(i); err != nil {
		a.setStatus("%s", err)
	}
	a.Wake()
}

// closingLines are what the panel says.
func (a *App) closingLines() [][2]string {
	c := a.closing
	if c == nil {
		return nil
	}
	what := fmt.Sprintf("Close %q?", c.title)
	kept := "The conversation keeps running, and stays in alt+space."
	if len(c.held) > 1 {
		kept = fmt.Sprintf("The %d conversations keep running, and stay in alt+space.",
			len(c.held))
	}
	return [][2]string{
		{"", what},
		{"", kept},
		{"", ""},
		{"", "End it instead and the process goes with the tab."},
		{"", "What it said is on disk either way, so it can be resumed."},
	}
}
