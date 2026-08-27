package app

import (
	"reflect"
	"testing"
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
