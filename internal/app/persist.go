package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"claudecontrol/internal/layout"
)

// PaneSnapshot is one pane, as it was when the application last changed shape.
type PaneSnapshot struct {
	Module    string `json:"module"`
	Dir       string `json:"dir,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// Snapshot is what the next run offers to bring back.
type Snapshot struct {
	Panes []PaneSnapshot `json:"panes"`
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

// saveSnapshot records the panes so a later run can bring them back.
//
// Failures are silent: this is a convenience, and a warning about it would sit
// on top of a live session.
func (a *App) saveSnapshot() {
	type sessioned interface{ SessionID() string }
	var snap Snapshot
	for _, id := range layout.Leaves(a.root) {
		m, ok := a.modules[id]
		if !ok {
			continue
		}
		p := PaneSnapshot{Module: "term"}
		if sm, ok := m.(sessioned); ok {
			p.Module = "claude"
			p.SessionID = sm.SessionID()
		}
		snap.Panes = append(snap.Panes, p)
	}
	_ = SaveSnapshot(SnapshotPath(), snap)
}
