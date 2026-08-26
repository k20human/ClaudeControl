// Package transcript follows the JSONL file Claude Code writes for a session.
//
// The path is never guessed from the working directory: two sessions in one
// directory write to the same folder, and a relocated session moves. It comes
// from the hook payload, which is authoritative.
package transcript

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// Usage is the token accounting of one assistant turn.
type Usage struct {
	Input         int `json:"input_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
	Output        int `json:"output_tokens"`
	Thinking      int `json:"-"`
}

// Line is one entry of a transcript, reduced to what is used.
type Line struct {
	Type  string
	Model string
	Usage Usage

	// AITitle is the name Claude Code gave the session. It arrives on its own
	// line type, not with a turn, and it is what a person recognises the
	// session by — the directory name only says where it runs.
	AITitle string
}

// raw mirrors the parts of the on-disk shape that are read.
type raw struct {
	Type    string `json:"type"`
	AITitle string `json:"aiTitle"`
	Message struct {
		Model string `json:"model"`
		Usage struct {
			Usage
			Details struct {
				Thinking int `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	} `json:"message"`
}

// Tailer follows one file.
type Tailer struct {
	ch   chan Line
	done chan struct{}
	once sync.Once
}

// Tail starts following path, polling every interval.
//
// Polling rather than a watch: the file may not exist yet, may be replaced,
// and one stat per session per interval is cheap. A watch would have to handle
// creation, replacement and deletion for no measurable gain.
func Tail(path string, interval time.Duration) *Tailer {
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	t := &Tailer{
		ch:   make(chan Line, 64),
		done: make(chan struct{}),
	}
	go t.run(path, interval)
	return t
}

// Lines carries the entries as they are appended.
func (t *Tailer) Lines() <-chan Line { return t.ch }

// Close stops following. It is safe to call more than once.
func (t *Tailer) Close() { t.once.Do(func() { close(t.done) }) }

func (t *Tailer) run(path string, interval time.Duration) {
	defer close(t.ch)

	var f *os.File
	var rd *bufio.Reader
	var offset int64
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()

	// pending holds a line that has arrived only in part. A transcript is
	// appended to while it is read, so a read can land mid-line; delivering
	// that as a truncated entry would report nonsense.
	var pending []byte

	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {
		case <-t.done:
			return
		case <-tick.C:
		}

		if f == nil {
			opened, err := os.Open(path)
			if err != nil {
				continue
			}
			f, rd, offset = opened, bufio.NewReader(opened), 0
		}

		// A shrunken file was replaced: start again rather than read from an
		// offset that now means something else.
		if st, err := f.Stat(); err == nil && st.Size() < offset {
			_, _ = f.Seek(0, io.SeekStart)
			rd.Reset(f)
			offset, pending = 0, nil
		}

		for {
			chunk, err := rd.ReadBytes('\n')
			offset += int64(len(chunk))
			if len(chunk) > 0 && chunk[len(chunk)-1] != '\n' {
				pending = append(pending, chunk...)
				break
			}
			if len(chunk) == 0 {
				break
			}
			line := chunk
			if len(pending) > 0 {
				line = append(pending, chunk...)
				pending = nil
			}
			if l, ok := parse(line); ok {
				select {
				case t.ch <- l:
				case <-t.done:
					return
				default: // nobody is reading; a stale figure beats a stall
				}
			}
			if err != nil {
				break
			}
		}
	}
}

// parse turns one JSONL line into a Line, reporting whether it was usable.
func parse(b []byte) (Line, bool) {
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return Line{}, false
	}
	if r.Type == "" {
		return Line{}, false
	}
	u := r.Message.Usage.Usage
	u.Thinking = r.Message.Usage.Details.Thinking
	return Line{Type: r.Type, Model: r.Message.Model, Usage: u, AITitle: r.AITitle}, true
}
