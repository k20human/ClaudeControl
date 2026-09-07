// Package tabs puts several modules in one pane and shows one at a time.
//
// It exists for a window that is not wide enough to split again. Splitting is
// better when there is room — you see two things at once — so this is what you
// reach for when there is not.
//
// Every module in the pane runs, whether or not it is the one on screen: a
// supervisor still supervises from a hidden tab, and a Claude session still
// answers. Only drawing is skipped.
package tabs

import (
	"fmt"
	"image/color"
	"sync"

	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/session"
	"claudecontrol/internal/settings"
	"claudecontrol/internal/transcript"
	"claudecontrol/internal/usage"
)

func init() { module.Register("tabs", New) }

// stripRows is the row the tab strip takes. The module refuses the pane title
// so that this row replaces it rather than adding to it — the point of tabs is
// that space is short.
const stripRows = 1

var (
	bgStrip   = color.RGBA{R: 0x1b, G: 0x21, B: 0x2b, A: 0xff}
	fgIdle    = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	bgActive  = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	fgActive  = color.RGBA{R: 0x10, G: 0x16, B: 0x1e, A: 0xff}
	fgWaiting = color.RGBA{R: 0xe0, G: 0xb0, B: 0x5c, A: 0xff}
	// The + is a control, not a label, and is filled so it reads as one.
	bgPlus = color.RGBA{R: 0x2c, G: 0x3b, B: 0x4d, A: 0xff}
	fgPlus = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
)

// tab is one module and what to call it.
type tab struct {
	title string
	// name is the module it was built from, kept so the pane can be written
	// back to the configuration as it stands rather than as it was declared.
	// opts is what it was built with, kept for the same reason and for a
	// harder one: a term cannot describe itself, and a tab written down as a
	// term with no options comes back as a bare shell.
	name string
	opts map[string]any
	mod  module.Module

	// x and w are where its label was last drawn, so a click lands on what
	// was seen rather than on a table that could have drifted from it.
	// closeX is where its cross was drawn, or zero if it had none.
	x, w, closeX int

	// state is what the session behind this tab is doing, so a tab you are not
	// looking at can still say so. Without it, a hidden tab is a session you
	// have forgotten.
	state pool.State
	// known is whether this tab holds a session at all. A hologram has no
	// state and must not be given the mark for one.
	known bool
}

// Module is the pane.
type Module struct {
	ctx module.Context

	// dir is where tabs opened in this pane start, when the caller does not
	// say. Empty means "wherever the tab on screen is working", which is the
	// answer that is right without anybody configuring anything.
	dir string

	mu     sync.Mutex
	tabs   []*tab
	active int

	cols, rows int
	// plusX is where the + was last drawn, pane-local.
	plusX int
}

// The two controls in the strip. Kept short: every cell they take is a cell a
// tab label does not have.
const (
	closeLabel = "×"
	plusLabel  = " + "
)

// New builds the module from its configuration.
func New(cfg map[string]any) (module.Module, error) {
	raw, listed := cfg["tabs"].([]any)
	m := &Module{}
	if v, ok := cfg["dir"].(string); ok {
		m.dir = expandDir(v)
	}
	for _, item := range raw {
		spec, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tabs: each tab must be a mapping, got %T", item)
		}
		name, _ := spec["module"].(string)
		if name == "" {
			return nil, fmt.Errorf("tabs: a tab has no %q", "module")
		}
		opts, _ := spec["options"].(map[string]any)
		child, err := module.New(name, opts)
		if err != nil {
			return nil, fmt.Errorf("tabs: %w", err)
		}
		title, _ := spec["title"].(string)
		if title == "" {
			title = name
		}
		m.tabs = append(m.tabs, &tab{title: title, name: name, opts: opts, mod: child})
	}
	if len(m.tabs) == 0 && !listed {
		// No tabs and nothing saying so is a mistake in the configuration,
		// and one worth naming: the pane would draw an empty strip for ever.
		//
		// An empty list, on the other hand, is deliberate. It is how the
		// application asks for a pane to pour tabs into — wrapping a pane
		// that was not one, when a tab is dropped on it.
		return nil, fmt.Errorf("tabs: no %q configured", "tabs")
	}
	return m, nil
}

// NewTabModule is what the + button creates. A session, because that is what
// you open a tab for; the same thing alt+n puts in a new pane.
const NewTabModule = "claude"

// Add builds a module and puts it in a new tab, which becomes the one on
// screen — opening a tab you then have to go and find would be a strange kind
// of opening.
func (m *Module) Add(name string, opts map[string]any) error {
	opts = m.withDir(opts)
	child, err := module.New(name, opts)
	if err != nil {
		return fmt.Errorf("tabs: %w", err)
	}
	if err := child.Init(m.ctx); err != nil {
		_ = child.Close()
		return fmt.Errorf("tabs: %s: %w", name, err)
	}

	m.mu.Lock()
	title := m.uniqueTitleLocked(titleFor(child, name))
	inner := m.rows - stripRows
	if inner < 1 {
		inner = 1
	}
	m.tabs = append(m.tabs, &tab{title: title, name: name, opts: opts, mod: child})
	m.active = len(m.tabs) - 1
	cols := m.cols
	m.mu.Unlock()

	// Resizing is what starts a hosted process, so its failure is the failure
	// to open the tab and has to be said. The tab stays — it is there, it is
	// simply empty — because removing it would leave nothing to explain.
	if cols > 0 {
		if err := child.Resize(cols, inner); err != nil && m.ctx.Status != nil {
			m.ctx.Status(title + ": " + err.Error())
		}
	}
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
	return nil
}

// titleFor asks the module what it would like to be called, and falls back on
// the name it was registered under.
func titleFor(child module.Module, name string) string {
	if t, ok := child.(interface{ Title() (string, bool) }); ok {
		if title, want := t.Title(); want && title != "" {
			return title
		}
	}
	return name
}

// uniqueTitleLocked numbers a title that is already taken. Three tabs all
// reading "claude" would be three tabs you cannot tell apart.
func (m *Module) uniqueTitleLocked(want string) string {
	return m.uniqueTitleExceptLocked(want, nil)
}

// uniqueTitleExceptLocked is the same, ignoring one tab — the one being
// renamed, which would otherwise collide with the name it already has.
func (m *Module) uniqueTitleExceptLocked(want string, self *tab) string {
	taken := func(s string) bool {
		for _, t := range m.tabs {
			if t != self && t.title == s {
				return true
			}
		}
		return false
	}
	if !taken(want) {
		return want
	}
	for n := 2; ; n++ {
		try := fmt.Sprintf("%s %d", want, n)
		if !taken(try) {
			return try
		}
	}
}

// CloseTab closes one and shows its neighbour.
//
// What closing does to whatever the tab held is the module's own business: a
// Claude session detaches and keeps running, reachable from the sessions list,
// while a shell ends. That is the same distinction closing a pane makes, and
// it would be strange for a tab to make a different one.
func (m *Module) CloseTab(i int) error {
	m.mu.Lock()
	if i < 0 || i >= len(m.tabs) {
		m.mu.Unlock()
		return fmt.Errorf("tabs: no tab %d", i)
	}
	if len(m.tabs) == 1 {
		m.mu.Unlock()
		// The pane would be left with nothing to draw. Closing the pane is a
		// different gesture, and the application already has one.
		return fmt.Errorf("tabs: this is the last tab; close the pane instead")
	}
	doomed := m.tabs[i]
	m.tabs = append(m.tabs[:i:i], m.tabs[i+1:]...)
	if m.active >= len(m.tabs) {
		m.active = len(m.tabs) - 1
	} else if m.active > i {
		m.active--
	}
	m.mu.Unlock()

	err := doomed.mod.Close()
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
	return err
}

// Count is how many tabs there are.
func (m *Module) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tabs)
}

// Active index, for whatever needs to close the one on screen.
func (m *Module) ActiveIndex() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// Init starts every module, not only the one that will be shown first.
func (m *Module) Init(ctx module.Context) error {
	m.ctx = ctx
	for _, t := range m.tabs {
		if err := t.mod.Init(ctx); err != nil {
			return fmt.Errorf("tabs: %s: %w", t.title, err)
		}
	}
	if ctx.Bus == nil {
		return nil
	}
	// The name Claude Code gave a conversation, once it has given one.
	//
	// A tab is named after its directory to begin with, which for three tabs
	// in one project reads "DEV", "DEV 2", "DEV 3" and says nothing about
	// which is which. The pane title has followed the session's real name from
	// the start; the strip that replaced the pane title has to as well.
	names, _ := ctx.Bus.SubscribeEvent(transcript.NameTopic, transcript.NameDepth)
	go func() {
		for v := range names {
			named, ok := v.(transcript.SessionName)
			if !ok || named.Name == "" {
				continue
			}
			if m.rename(named.SessionID, named.Name) && ctx.Wake != nil {
				ctx.Wake()
			}
		}
	}()

	// A session that wants something while you are on another tab has to be
	// able to say so.
	states := ctx.Bus.SubscribeState(pool.StateTopic)
	go func() {
		for v := range states {
			entries, ok := v.([]*pool.Entry)
			if !ok {
				continue
			}
			if m.noteStates(entries) && ctx.Wake != nil {
				ctx.Wake()
			}
		}
	}()
	return nil
}

// noteStates records what each tab's session is doing, and reports whether
// anything changed.
func (m *Module) noteStates(entries []*pool.Entry) bool {
	states := map[string]pool.State{}
	for _, e := range entries {
		if e.Session != nil {
			states[string(e.Session.ID)] = e.State
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	for _, t := range m.tabs {
		s, ok := t.mod.(interface{ SessionID() string })
		if !ok {
			continue
		}
		st, held := states[s.SessionID()]
		if t.state != st || t.known != held {
			t.state, t.known, changed = st, held, true
		}
	}
	return changed
}

// Title refuses the pane title: the strip takes that row instead of adding one.
func (m *Module) Title() (string, bool) { return "", false }

// Resize sizes every module, shown or not, so switching to one never shows it
// laid out for a size it no longer has.
func (m *Module) Resize(w, h int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cols, m.rows = w, h
	inner := h - stripRows
	if inner < 1 {
		inner = 1
	}
	for _, t := range m.tabs {
		if err := t.mod.Resize(w, inner); err != nil {
			return err
		}
	}
	return nil
}

// Close closes every module.
func (m *Module) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	for _, t := range m.tabs {
		if err := t.mod.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Active is the module on screen.
func (m *Module) Active() module.Module {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tabs) == 0 {
		return nil
	}
	return m.tabs[m.active].mod
}

// ActiveTitle is what the tab on screen is called.
func (m *Module) ActiveTitle() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tabs) == 0 {
		return ""
	}
	return m.tabs[m.active].title
}

// Select shows a tab by index.
func (m *Module) Select(i int) {
	m.mu.Lock()
	if i < 0 || i >= len(m.tabs) || i == m.active {
		m.mu.Unlock()
		return
	}
	m.active = i
	m.mu.Unlock()
	if m.ctx.Wake != nil {
		m.ctx.Wake()
	}
}

// CycleTab moves by delta, wrapping. It is what the application's tab
// shortcuts call.
func (m *Module) CycleTab(delta int) {
	m.mu.Lock()
	n := len(m.tabs)
	if n == 0 {
		m.mu.Unlock()
		return
	}
	next := ((m.active+delta)%n + n) % n
	m.mu.Unlock()
	m.Select(next)
}

// A pane of tabs stands between the application and the modules it holds, so
// everything the application looks for on a pane has to be passed through.
// What follows is that pass-through. Forgetting one is not a compile error —
// it is a feature that quietly stops working the day someone puts the module
// in a tab, which is exactly how the account figures left the status bar.

// Account is the budget reading of whichever tab is fetching one.
//
// Every tab is asked, not only the one on screen: a stats module in a tab you
// are not looking at is still reading, and the bar should still say so.
func (m *Module) Account() (usage.Reading, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tabs {
		acc, ok := t.mod.(interface {
			Account() (usage.Reading, bool)
		})
		if !ok {
			continue
		}
		if r, taken := acc.Account(); taken {
			return r, true
		}
	}
	return usage.Reading{}, false
}

// SessionID is the session of the tab on screen, if it holds one.
// Session is the process behind the tab on screen, if it has one.
//
// The application asks a pane whether its process is gone, and draws the
// banner saying so on the pane that answers yes. Without this a conversation
// that ended inside a tab left a dead screen with nothing said about it — the
// pane held the answer and never passed the question on.
func (m *Module) Session() *session.Session {
	if s, ok := m.Active().(interface{ Session() *session.Session }); ok {
		return s.Session()
	}
	return nil
}

func (m *Module) SessionID() string {
	s, ok := m.Active().(interface{ SessionID() string })
	if !ok {
		return ""
	}
	return s.SessionID()
}

// Sessions are the conversations in every tab, in order — not only the one on
// screen. A tab you were not looking at is still one you want back.
// rename gives a tab the name Claude Code gave its conversation, and reports
// whether anything changed.
func (m *Module) rename(id, name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tabs {
		holder, ok := t.mod.(interface{ SessionID() string })
		if !ok || holder.SessionID() != id {
			continue
		}
		if t.title == name {
			return false
		}
		t.title = m.uniqueTitleExceptLocked(name, t)
		return true
	}
	return false
}

// SelectSession brings the tab holding a conversation to the front, and
// reports whether it found one. It is how the application acts on a session
// rather than on a pane: what is waiting on you is a conversation, and the tab
// it happens to be in is an implementation detail of where you put it.
func (m *Module) SelectSession(id string) bool {
	m.mu.Lock()
	found := -1
	for i, t := range m.tabs {
		l, ok := t.mod.(module.Sessioner)
		if !ok {
			continue
		}
		for _, held := range l.Sessions() {
			if held == id {
				found = i
				break
			}
		}
		if found >= 0 {
			break
		}
	}
	m.mu.Unlock()
	if found < 0 {
		return false
	}
	m.Select(found)
	return true
}

func (m *Module) Sessions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, t := range m.tabs {
		if l, ok := t.mod.(module.Sessioner); ok {
			out = append(out, l.Sessions()...)
		}
	}
	return out
}

// finder is what a pane must offer for its history to be searched.
type finder interface {
	Find(string) int
	FindNext(int)
	FindClear()
	FindStatus() (string, int, int)
}

// Find, FindNext, FindClear and FindStatus reach the tab on screen. Searching
// a tab you are not looking at would move a view you cannot see.
// CanFind reports whether the tab on screen has anything to search. A tab is
// what the search would run over, so a tab with no output means no search —
// the strip is not what you are looking through.
func (m *Module) CanFind() bool {
	active := m.Active()
	if c, ok := active.(interface{ CanFind() bool }); ok {
		return c.CanFind()
	}
	_, ok := active.(finder)
	return ok
}

func (m *Module) Find(query string) int {
	f, ok := m.Active().(finder)
	if !ok {
		return 0
	}
	return f.Find(query)
}

func (m *Module) FindNext(delta int) {
	if f, ok := m.Active().(finder); ok {
		f.FindNext(delta)
	}
}

func (m *Module) FindClear() {
	if f, ok := m.Active().(finder); ok {
		f.FindClear()
	}
}

func (m *Module) FindStatus() (string, int, int) {
	f, ok := m.Active().(finder)
	if !ok {
		return "", 0, 0
	}
	return f.FindStatus()
}

// SelectedText is what is selected in the tab on screen.
func (m *Module) SelectedText() string {
	s, ok := m.Active().(interface{ SelectedText() string })
	if !ok {
		return ""
	}
	return s.SelectedText()
}

// ScrollOffset is how far back the tab on screen is.
func (m *Module) ScrollOffset() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scrollOffsetLocked()
}

// scrollOffsetLocked exists because the strip needs this while it is already
// holding the lock to draw, and a Go mutex is not reentrant.
func (m *Module) scrollOffsetLocked() int {
	if len(m.tabs) == 0 {
		return 0
	}
	s, ok := m.tabs[m.active].mod.(interface{ ScrollOffset() int })
	if !ok {
		return 0
	}
	return s.ScrollOffset()
}

// Values is the pane as it stands, for writing back to the configuration.
//
// As it stands, not as it was declared: a tab opened while you worked is part
// of the pane now, and saving a configuration that omitted it would lose it.
func (m *Module) Values() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]any, 0, len(m.tabs))
	for _, t := range m.tabs {
		entry := map[string]any{"title": t.title, "module": t.name}
		// What it was built with, so a tab whose module cannot speak for
		// itself still comes back as itself rather than as a bare shell.
		if len(t.opts) > 0 {
			entry["options"] = t.opts
		}
		if v, ok := t.mod.(interface{ Values() map[string]any }); ok {
			entry["options"] = v.Values()
		}
		out = append(out, entry)
	}
	return map[string]any{"tabs": out}
}

// Settings forwards to the module on screen, so the settings menu configures
// what you are looking at.
func (m *Module) Settings() []settings.Setting {
	if p, ok := m.Active().(module.Provider); ok {
		return p.Settings()
	}
	return nil
}
