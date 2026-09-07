package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
)

// Conversations are handed out in the order the panes appear, which is the
// order they were recorded in.
func TestSessionsAreHandedOutInOrder(t *testing.T) {
	pending := []string{"first", "second"}

	a := withResume("claude", map[string]any{"dir": "/one"}, &pending)
	b := withResume("claude", map[string]any{"dir": "/two"}, &pending)
	c := withResume("claude", nil, &pending)

	if a["resume"] != "first" || a["dir"] != "/one" {
		t.Errorf("the first pane got %v", a)
	}
	if b["resume"] != "second" {
		t.Errorf("the second pane got %v", b)
	}
	// More panes than conversations: the extras start fresh rather than
	// resuming something arbitrary.
	if _, set := c["resume"]; set {
		t.Errorf("a third pane was given a conversation from nowhere: %v", c)
	}
}

// A shell consumes nothing: a fresh shell is not something to bring back, and
// taking an id here would hand the next pane the wrong conversation.
func TestOnlyConversationsConsumeAnID(t *testing.T) {
	pending := []string{"only"}
	withResume("term", map[string]any{"cmd": []any{"zsh"}}, &pending)
	withResume("hologram", nil, &pending)
	if len(pending) != 1 {
		t.Fatalf("something other than a conversation took an id: %v", pending)
	}
	if got := withResume("claude", nil, &pending); got["resume"] != "only" {
		t.Errorf("the conversation went to %v", got)
	}
}

// A resume written in the configuration by hand was put there on purpose.
func TestAHandWrittenResumeWins(t *testing.T) {
	pending := []string{"recorded"}
	got := withResume("claude", map[string]any{"resume": "chosen"}, &pending)
	if got["resume"] != "chosen" {
		t.Errorf("resume = %v, want the one from the file", got["resume"])
	}
	if len(pending) != 1 {
		t.Errorf("the recorded id was consumed anyway: %v", pending)
	}
}

// A pane of tabs holds conversations too, and each of its tabs takes its turn
// in the same order.
func TestTabsTakeTheirTurn(t *testing.T) {
	pending := []string{"one", "two"}
	opts := map[string]any{"tabs": []any{
		map[string]any{"title": "a", "module": "claude", "options": map[string]any{"dir": "/a"}},
		map[string]any{"title": "b", "module": "term"},
		map[string]any{"title": "c", "module": "claude"},
	}}

	got := withResume("tabs", opts, &pending)
	tabs, _ := got["tabs"].([]any)
	if len(tabs) != 3 {
		t.Fatalf("the tabs were lost: %v", got)
	}
	first, _ := tabs[0].(map[string]any)["options"].(map[string]any)
	third, _ := tabs[2].(map[string]any)["options"].(map[string]any)
	if first["resume"] != "one" || first["dir"] != "/a" {
		t.Errorf("the first tab got %v", first)
	}
	if third["resume"] != "two" {
		t.Errorf("the third tab got %v", third)
	}
	if len(pending) != 0 {
		t.Errorf("%v was left over", pending)
	}

	// The configuration the caller passed in is not modified: it is read again
	// when the settings are written back.
	inner, _ := opts["tabs"].([]any)[0].(map[string]any)["options"].(map[string]any)
	if _, set := inner["resume"]; set {
		t.Error("withResume wrote into the configuration it was given")
	}
}

// Nothing recorded means nothing changed, down to the map itself.
func TestNoRecordedSessionsChangesNothing(t *testing.T) {
	var pending []string
	opts := map[string]any{"dir": "/one"}
	if got := withResume("claude", opts, &pending); !reflect.DeepEqual(got, opts) {
		t.Errorf("withResume = %v, want %v", got, opts)
	}
}

// A session that was opened and never spoken to has no transcript, and asking
// Claude Code to resume it fails with an error the person can do nothing
// about. Dropping it turns that into a fresh pane, which is what they wanted.
func TestOnlySessionsWithATranscriptAreResumed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	project := filepath.Join(dir, "projects", "-home-k-api")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "spoke.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := resumable([]string{"never-spoke", "spoke", "also-never"})
	if len(got) != 1 || got[0] != "spoke" {
		t.Errorf("resumable = %v, want only the one with a transcript", got)
	}

	// And the survivor still goes to the first pane, rather than the gap
	// leaving a pane resuming nothing while a later one gets it.
	pending := got
	first := withResume("claude", nil, &pending)
	if first["resume"] != "spoke" {
		t.Errorf("the first pane got %v", first)
	}
}

// The conversations and the arrangement do not change together: opening a tab
// adds one and leaves the layout exactly as it was. Recording only on a layout
// change meant a conversation opened in a tab was never written down, and
// closing the application lost it.
func TestTheSnapshotFollowsTheConversationsNotTheLayout(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("XDG_STATE_HOME", filepath.Dir(filepath.Dir(state)))

	a := &App{
		modules:     map[layout.PaneID]module.Module{},
		moduleNames: map[layout.PaneID]string{},
		root:        &layout.Node{Kind: layout.KindLeaf, PaneID: 1},
	}
	holder := &sessionHolder{ids: []string{"first"}}
	a.modules[1] = holder

	a.saveSnapshot()
	got, err := LoadSnapshot(SnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0] != "first" {
		t.Fatalf("after the first save: %v", got.Sessions)
	}

	// A tab opens. The layout has not moved.
	holder.ids = []string{"first", "second"}
	a.saveSnapshot()
	got, _ = LoadSnapshot(SnapshotPath())
	if len(got.Sessions) != 2 || got.Sessions[1] != "second" {
		t.Errorf("a conversation opened in a tab was not recorded: %v", got.Sessions)
	}

	// And nothing changing writes nothing: the file keeps its modification
	// time, which is what makes a per-frame call acceptable.
	before, err := os.Stat(SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	a.saveSnapshot()
	after, err := os.Stat(SnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged set of conversations rewrote the file")
	}
}

// sessionHolder is a module that holds whatever conversations it is told to.
type sessionHolder struct{ ids []string }

func (s *sessionHolder) Init(module.Context) error    { return nil }
func (s *sessionHolder) Resize(int, int) error        { return nil }
func (s *sessionHolder) Draw(uv.Screen, uv.Rectangle) {}
func (s *sessionHolder) Close() error                 { return nil }
func (s *sessionHolder) Sessions() []string           { return s.ids }

// Resuming a conversation makes Claude Code write a new transcript under a new
// id. What a later run has to resume is that new one, not the id the pane was
// started with — which is how a resumed conversation came back as nothing.
func TestTheSnapshotRecordsWhatClaudeCodeCallsItNow(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	a := &App{
		modules:     map[layout.PaneID]module.Module{},
		moduleNames: map[layout.PaneID]string{},
		root:        &layout.Node{Kind: layout.KindLeaf, PaneID: 1},
	}
	a.modules[1] = &sessionHolder{ids: []string{"ours"}}

	a.saveSnapshot()
	got, _ := LoadSnapshot(SnapshotPath())
	if len(got.Sessions) != 1 || got.Sessions[0] != "ours" {
		t.Fatalf("before any hook: %v", got.Sessions)
	}

	// A hook arrives naming the pane, and saying Claude Code has moved on.
	a.noteLiveSession("ours", "claude-made-a-new-one")
	a.saveSnapshot()
	got, _ = LoadSnapshot(SnapshotPath())
	if len(got.Sessions) != 1 || got.Sessions[0] != "claude-made-a-new-one" {
		t.Errorf("after the hook: %v, want the conversation that is actually running", got.Sessions)
	}

	// A pane no hook has spoken for keeps its own id, which is the one it was
	// started with and the one there is to resume.
	if got := a.liveSession("never-heard-of"); got != "never-heard-of" {
		t.Errorf("liveSession invented %q", got)
	}
}

// A pane that names its own conversation takes it off the list.
//
// The saved arrangement and the recorded list come from the same run and name
// the same conversations. Taking the id from the arrangement and leaving it in
// the list gave the next pane without a conversation of its own the one this
// pane was already showing: two panes resuming one conversation, two Claude
// Code processes writing to it, and — because the state of a session is keyed
// on the identity they now share — one working mark lighting both tabs.
func TestAConversationClaimedByAPaneIsNotHandedOutAgain(t *testing.T) {
	pending := []string{"shared", "other"}

	first := withResume("claude", map[string]any{"resume": "shared"}, &pending)
	second := withResume("claude", nil, &pending)

	if first["resume"] != "shared" {
		t.Errorf("the pane lost the conversation it named: %v", first)
	}
	if second["resume"] == "shared" {
		t.Fatalf("two panes were given the same conversation: %v and %v", first, second)
	}
	if second["resume"] != "other" {
		t.Errorf("the next pane got %v, want the next conversation on the list", second["resume"])
	}
}

// The same thing one level down, which is where it actually happened: the
// panes were tabs.
func TestATabThatNamesItsConversationTakesItOffTheList(t *testing.T) {
	pending := []string{"shared", "other"}
	opts := map[string]any{"tabs": []any{
		map[string]any{"module": "claude", "options": map[string]any{"resume": "shared"}},
		map[string]any{"module": "claude"},
	}}

	got := withResume("tabs", opts, &pending)
	tabs, _ := got["tabs"].([]any)
	if len(tabs) != 2 {
		t.Fatalf("the tabs were lost: %v", got)
	}
	first, _ := tabs[0].(map[string]any)["options"].(map[string]any)
	second, _ := tabs[1].(map[string]any)["options"].(map[string]any)
	if first["resume"] != "shared" {
		t.Errorf("the first tab lost its conversation: %v", first)
	}
	if second["resume"] == "shared" {
		t.Fatalf("both tabs were given %q", "shared")
	}
	if second["resume"] != "other" {
		t.Errorf("the second tab got %v", second["resume"])
	}
}

// A conversation is one thing, so it is offered once however many times it was
// recorded — and a recorded id with no transcript is not offered at all.
func TestARecordedConversationIsOfferedOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	folder := filepath.Join(dir, "projects", "somewhere")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"kept", "also-kept"} {
		if err := os.WriteFile(filepath.Join(folder, id+".jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := resumable([]string{"kept", "kept", "also-kept", "never-spoken-to"})
	want := []string{"kept", "also-kept"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resumable = %v, want %v", got, want)
	}
}
