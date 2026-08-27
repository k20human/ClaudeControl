package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
)

// Snapshot is what a run leaves for the next one.
//
// A flat list of session ids rather than a picture of the layout. The
// conversations are what closing the application costs you; the arrangement is
// in the configuration file and comes back on its own. A list also survives
// rearranging the panes between runs, where anything shaped like the layout
// would not.
type Snapshot struct {
	// Sessions are the Claude sessions that were open, in the order their
	// panes appear. The next run hands them out in that order.
	Sessions []string `json:"sessions"`
}

// SnapshotPath is where the snapshot lives.
//
// A separate file from the configuration, and JSON rather than YAML. The
// configuration belongs to the user and keeps its comments; this belongs to
// the machine and is rewritten whenever the layout changes. Mixing the two
// would mean a program editing a file a person also edits.
func SnapshotPath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "state.json"
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "claudecontrol", "state.json")
}

// SaveSnapshot writes the snapshot, creating its directory.
func SaveSnapshot(path string, s Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("app: snapshot directory: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("app: encode snapshot: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("app: write snapshot: %w", err)
	}
	// Rename rather than write in place: a crash halfway through must not
	// leave a half-written snapshot where a good one used to be.
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("app: replace snapshot: %w", err)
	}
	return nil
}

// LoadSnapshot reads the snapshot. A missing or unreadable file yields an
// empty snapshot and no error: this is convenience the machine wrote, and
// losing it must never stop a run.
func LoadSnapshot(path string) (Snapshot, error) {
	var s Snapshot
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return Snapshot{}, nil
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return Snapshot{}, nil
	}
	return s, nil
}

// saveSnapshot records the open conversations so a later run can bring them
// back, when they are not what was last recorded.
//
// Called from the draw loop rather than from the layout, because the two do
// not change together: opening a tab adds a conversation and leaves the
// arrangement exactly as it was. Tying this to the layout meant a conversation
// opened in a tab was never written down, and closing the application lost it
// — which is what happened.
//
// The comparison is what makes a per-frame call cheap: a handful of interface
// calls and a string compare, and a write only when something actually
// changed.
//
// Failures are silent: this is a convenience, and a warning about it would sit
// on top of a live session.
func (a *App) saveSnapshot() {
	var snap Snapshot
	for _, id := range layout.Leaves(a.root) {
		m, ok := a.modules[id]
		if !ok {
			continue
		}
		// Sessions rather than SessionID: a pane may hold several, and a pane
		// of tabs holds whatever its tabs do. A shell reports none — a fresh
		// shell is not something to bring back.
		lister, ok := m.(module.Sessioner)
		if !ok {
			continue
		}
		for _, id := range lister.Sessions() {
			// What Claude Code is calling it now, which is what there will be
			// to resume — not the id we handed it when it started.
			snap.Sessions = append(snap.Sessions, a.liveSession(id))
		}
	}
	if sameStrings(a.savedSessions, snap.Sessions) {
		return
	}
	a.savedSessions = snap.Sessions
	_ = SaveSnapshot(SnapshotPath(), snap)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
