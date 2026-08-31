package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The saved arrangement is the one you get back, and it is written in the
// shape the configuration uses so the same code builds it.
func TestASavedLayoutIsUsedWhenItIsNewerThanTheConfiguration(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("layout:\n  module: term\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Written a moment after the configuration was last touched.
	saved := Snapshot{
		Layout:   map[string]any{"module": "hologram"},
		LayoutAt: time.Now().Add(time.Hour),
	}

	got, why := chooseLayout(saved, cfg)
	if got == nil {
		t.Fatalf("the saved layout was not used: %s", why)
	}
	if got["module"] != "hologram" {
		t.Errorf("got %v, want the saved arrangement", got)
	}
}

// Editing the configuration has to do something, or a setting that seems not
// to work is worse than one that does not exist.
func TestTheConfigurationWinsWhenItHasBeenEditedSince(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("layout:\n  module: term\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved := Snapshot{
		Layout:   map[string]any{"module": "hologram"},
		LayoutAt: time.Now().Add(-time.Hour),
	}

	got, why := chooseLayout(saved, cfg)
	if got != nil {
		t.Errorf("the saved layout won over a configuration edited since: %v", got)
	}
	if why == "" {
		t.Error("nothing was said about why")
	}
}

// Nothing saved is the ordinary first run.
func TestWithNothingSavedTheConfigurationIsUsed(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("layout:\n  module: term\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := chooseLayout(Snapshot{}, cfg); got != nil {
		t.Errorf("something was used with nothing saved: %v", got)
	}
}

// A configuration that is not there at all cannot have been edited since.
func TestAMissingConfigurationDoesNotBeatASavedLayout(t *testing.T) {
	saved := Snapshot{
		Layout:   map[string]any{"module": "hologram"},
		LayoutAt: time.Now(),
	}
	got, _ := chooseLayout(saved, filepath.Join(t.TempDir(), "nothing.yaml"))
	if got == nil {
		t.Error("the saved layout was dropped because no configuration exists")
	}
}

// The session list is written whenever a conversation opens or closes. It must
// not carry the arrangement away with it: the two answer different questions,
// and losing one to save the other would be a poor trade.
func TestSavingTheSessionsKeepsTheSavedLayout(t *testing.T) {
	// The application first: its fixture moves the state directory, and
	// seeding the old one would seed a file nobody reads.
	a := newTestApp(t)
	path := SnapshotPath()
	if err := SaveSnapshot(path, Snapshot{
		Sessions: []string{"before"},
		Layout:   map[string]any{"module": "hologram"},
		LayoutAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	a.savedSessions = nil
	a.saveSnapshot()

	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Layout) == 0 {
		t.Error("saving the sessions threw the arrangement away")
	}
	if got.LayoutAt.IsZero() {
		t.Error("the moment the arrangement was saved is gone")
	}
}

// Clearing the box is a decision, not silence: the arrangement that was saved
// has to go, or it comes back tomorrow.
func TestQuittingWithTheBoxClearedForgetsTheSavedLayout(t *testing.T) {
	a := newTestApp(t)
	path := SnapshotPath()
	if err := SaveSnapshot(path, Snapshot{
		Layout:   map[string]any{"module": "hologram"},
		LayoutAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	a.keepLayout = false
	a.saveLayout()

	if got, _ := LoadSnapshot(path); len(got.Layout) != 0 {
		t.Errorf("the arrangement survived being unticked: %v", got.Layout)
	}
}

// And ticked, it writes what is on screen.
func TestQuittingWithTheBoxTickedWritesTheArrangement(t *testing.T) {
	a := newTestApp(t)
	a.moduleNames[1] = "hologram"
	a.keepLayout = true
	a.saveLayout()

	got, _ := LoadSnapshot(SnapshotPath())
	if got.Layout == nil || got.Layout["module"] != "hologram" {
		t.Errorf("layout = %v, want the pane that was on screen", got.Layout)
	}
	if got.LayoutAt.IsZero() {
		t.Error("nothing says when it was saved")
	}
}
