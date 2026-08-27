package app

import (
	"os"
	"path/filepath"
	"testing"
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
