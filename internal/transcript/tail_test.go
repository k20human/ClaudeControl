package transcript_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claudecontrol/internal/transcript"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
}

func recv(t *testing.T, ch <-chan transcript.Line, what string) transcript.Line {
	t.Helper()
	select {
	case l := <-ch:
		return l
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return transcript.Line{}
	}
}

const assistantLine = `{"type":"assistant","message":{"model":"claude-opus-5","usage":{"input_tokens":2,"cache_creation_input_tokens":14519,"cache_read_input_tokens":22460,"output_tokens":689,"output_tokens_details":{"thinking_tokens":484}}}}` + "\n"

func TestTailerReadsAssistantLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, p, assistantLine)

	tl := transcript.Tail(p, 20*time.Millisecond)
	defer tl.Close()

	got := recv(t, tl.Lines(), "the first line")
	if got.Model != "claude-opus-5" {
		t.Errorf("Model = %q, want claude-opus-5", got.Model)
	}
	if got.Usage.CacheRead != 22460 || got.Usage.Thinking != 484 {
		t.Errorf("Usage = %+v, want cache_read 22460 and thinking 484", got.Usage)
	}
}

// A transcript grows while it is being read. The tailer must pick up what is
// appended without re-reading what it already delivered.
func TestTailerFollowsAppends(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, p, assistantLine)

	tl := transcript.Tail(p, 20*time.Millisecond)
	defer tl.Close()
	recv(t, tl.Lines(), "the first line")

	write(t, p, `{"type":"assistant","message":{"model":"claude-sonnet-5","usage":{"output_tokens":7}}}`+"\n")
	got := recv(t, tl.Lines(), "the appended line")
	if got.Model != "claude-sonnet-5" {
		t.Errorf("Model = %q, want the appended line", got.Model)
	}
}

// A file that does not exist yet is normal: the tailer is started from a hook
// payload, and Claude Code may not have written anything at that point.
func TestTailerWaitsForTheFileToAppear(t *testing.T) {
	p := filepath.Join(t.TempDir(), "later.jsonl")
	tl := transcript.Tail(p, 20*time.Millisecond)
	defer tl.Close()

	time.Sleep(100 * time.Millisecond)
	write(t, p, assistantLine)
	if got := recv(t, tl.Lines(), "the line written after the tailer started"); got.Model == "" {
		t.Fatal("nothing was read once the file appeared")
	}
}

// A half-written line must not be delivered as a truncated one, nor block
// everything after it.
func TestTailerWaitsForACompleteLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, p, `{"type":"assistant","message":{"model":"claude-`)

	tl := transcript.Tail(p, 20*time.Millisecond)
	defer tl.Close()

	select {
	case l := <-tl.Lines():
		t.Fatalf("a partial line was delivered: %+v", l)
	case <-time.After(200 * time.Millisecond):
	}

	write(t, p, `opus-5","usage":{"output_tokens":1}}}`+"\n")
	if got := recv(t, tl.Lines(), "the completed line"); got.Model != "claude-opus-5" {
		t.Errorf("Model = %q, want the line once it was completed", got.Model)
	}
}

// Nothing may block on a reader that has gone away.
func TestCloseIsSafeWhileNobodyReads(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	write(t, p, assistantLine+assistantLine+assistantLine)
	tl := transcript.Tail(p, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	tl.Close()
	tl.Close() // idempotent
}
