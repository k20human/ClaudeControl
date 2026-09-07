package tabs

import "strings"

// Naming a tab by hand.
//
// Claude Code names a conversation after what it is about, which is what you
// want nearly always and wrong exactly when you disagree with it. So a name
// you type wins: nothing published afterwards overwrites it, and the
// application writes it down against the conversation rather than against the
// tab, so it comes back where the conversation does.

// Rename gives a tab a name you typed, and reports whether anything changed.
//
// An empty name is not a name. It hands the tab back: the automatic name
// applies again, starting with the one the module gives itself.
func (m *Module) Rename(i int, title string) bool {
	title = strings.TrimSpace(title)
	m.mu.Lock()
	defer m.mu.Unlock()
	if i < 0 || i >= len(m.tabs) {
		return false
	}
	t := m.tabs[i]
	if title == "" {
		if !t.given {
			return false
		}
		t.given = false
		t.title = m.uniqueTitleExceptLocked(titleFor(t.mod, t.name), t)
		return true
	}
	t.given = true
	if t.title == title {
		return false
	}
	t.title = m.uniqueTitleExceptLocked(title, t)
	return true
}

// TitleAt is what a tab is called, so a prompt can start from the name it
// already has rather than from an empty line.
func (m *Module) TitleAt(i int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i < 0 || i >= len(m.tabs) {
		return ""
	}
	return m.tabs[i].title
}

// SessionAt is the conversation a tab holds, or "" for a tab that holds none.
//
// A name is remembered against the conversation, not against the tab's place
// in the strip: tabs are opened, closed and dragged about, and a name pinned
// to the third slot would come back tomorrow on somebody else's conversation.
func (m *Module) SessionAt(i int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i < 0 || i >= len(m.tabs) {
		return ""
	}
	holder, ok := m.tabs[i].mod.(interface{ SessionID() string })
	if !ok {
		return ""
	}
	return holder.SessionID()
}

// GivenNames are the names you typed, by conversation — what there is to write
// down at the end of a run. A tab wearing a name it derived reports nothing:
// that name is derived again next time and does not need keeping.
func (m *Module) GivenNames() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out map[string]string
	for _, t := range m.tabs {
		if !t.given {
			continue
		}
		holder, ok := t.mod.(interface{ SessionID() string })
		if !ok {
			continue
		}
		id := holder.SessionID()
		if id == "" {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(m.tabs))
		}
		out[id] = t.title
	}
	return out
}

// NameSession gives the tab holding a conversation a name you typed, and
// reports whether it found one. It is how a name written down last time is put
// back: what comes back is the conversation, and the tab it lands in is
// whichever one took it.
func (m *Module) NameSession(id, title string) bool {
	if id == "" || strings.TrimSpace(title) == "" {
		return false
	}
	m.mu.Lock()
	at := -1
	for i, t := range m.tabs {
		holder, ok := t.mod.(interface{ SessionID() string })
		if ok && holder.SessionID() == id {
			at = i
			break
		}
	}
	m.mu.Unlock()
	if at < 0 {
		return false
	}
	return m.Rename(at, title)
}
