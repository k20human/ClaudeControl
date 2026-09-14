package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claudecontrol/internal/config"
)

func TestSnapshotRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	want := Snapshot{Sessions: []string{"aaa", "bbb"}}
	if err := SaveSnapshot(p, want); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	got, err := LoadSnapshot(p)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(got.Sessions) != 2 || got.Sessions[1] != "bbb" {
		t.Fatalf("LoadSnapshot = %+v, want %+v", got, want)
	}
}

// A first run has no snapshot, and that is not a failure.
func TestLoadSnapshotOfAMissingFileIsEmpty(t *testing.T) {
	got, err := LoadSnapshot(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(got.Sessions) != 0 {
		t.Fatalf("LoadSnapshot = %+v, want an empty snapshot", got)
	}
}

// A corrupt snapshot must not stop the application: it is machine-written
// convenience, not the user's configuration.
func TestACorruptSnapshotIsTreatedAsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSnapshot(p)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(got.Sessions) != 0 {
		t.Fatalf("LoadSnapshot = %+v, want an empty snapshot", got)
	}
}

// Writing must be atomic: a crash halfway through must not leave a truncated
// snapshot where a valid one used to be.
func TestSaveSnapshotLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	if err := SaveSnapshot(p, Snapshot{Sessions: []string{"aaa"}}); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("directory holds %v, want only state.json", names)
	}
}

// Editing the configuration must not cost you the arrangement you left, unless
// what you edited was the arrangement.
//
// The rule compared the file's date, so adding a service — or the key that
// says what every session is started with — threw away an afternoon of
// dragging panes about. The question is about the panes, so that is what is
// asked.
func TestOnlyALayoutEditOverridesTheSavedArrangement(t *testing.T) {
	saved := map[string]any{"module": "term"}
	before := config.LayoutFingerprint(&config.NodeSpec{
		Split:  "horizontal",
		Ratios: []int{1, 1},
		Children: []*config.NodeSpec{
			{Module: "claude", Options: map[string]any{"dir": "/home/k20/DEV"}},
			{Module: "hologram"},
		},
	})
	snap := Snapshot{Layout: saved, LayoutFrom: before, LayoutAt: time.Now().Add(-time.Hour)}

	// The file has been edited — a key about sessions, nothing about panes —
	// and the layout it asks for is word for word the one it asked for before.
	same := config.LayoutFingerprint(&config.NodeSpec{
		Split:  "horizontal",
		Ratios: []int{1, 1},
		Children: []*config.NodeSpec{
			{Module: "claude", Options: map[string]any{"dir": "/home/k20/DEV"}},
			{Module: "hologram"},
		},
	})
	got, why := chooseLayout(snap, "config.yaml", same)
	if got == nil {
		t.Errorf("the arrangement was thrown away for an edit that moved no pane: %s", why)
	}

	// And a pane added to the file does win.
	changed := config.LayoutFingerprint(&config.NodeSpec{
		Split:  "horizontal",
		Ratios: []int{1, 1, 1},
		Children: []*config.NodeSpec{
			{Module: "claude", Options: map[string]any{"dir": "/home/k20/DEV"}},
			{Module: "hologram"},
			{Module: "stats"},
		},
	})
	if got, why := chooseLayout(snap, "config.yaml", changed); got != nil {
		t.Errorf("a pane added to the configuration did nothing (%q)", why)
	} else if why == "" {
		t.Error("the configuration won without saying why")
	}
}

// The same options, read twice, fingerprint the same. A map iterated in
// whatever order Go felt like would make every start look like an edit.
func TestTheFingerprintIsStableAcrossReads(t *testing.T) {
	spec := func() *config.NodeSpec {
		return &config.NodeSpec{Module: "supervisor", Options: map[string]any{
			"keep_running": true,
			"stop_grace":   5,
			"services": []any{
				map[string]any{"name": "api", "cmd": []any{"npm", "run", "debug"}},
				map[string]any{"name": "web", "cmd": []any{"npm", "run", "dev"}},
			},
		}}
	}
	first := config.LayoutFingerprint(spec())
	for i := 0; i < 20; i++ {
		if got := config.LayoutFingerprint(spec()); got != first {
			t.Fatalf("read %d fingerprinted as %q, want %q", i, got, first)
		}
	}
	if first == "" {
		t.Fatal("no fingerprint at all")
	}
}
