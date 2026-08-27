// Package hooks carries Claude Code's hook events into the application.
//
// A hook is a short-lived process spawned by Claude Code, so it needs a way to
// reach a running ClaudeControl. It gets one by being ClaudeControl: the hook
// command is our own binary in --hook mode, which reads the payload on its
// standard input and writes one line to a Unix socket. No helper script to
// install, nothing external to depend on, and a few milliseconds per event.
package hooks

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnvSocket names the environment variable through which a session learns
// where to send its hook payloads.
const EnvSocket = "CLAUDECONTROL_HOOK_SOCKET"

// sendTimeout bounds a hook's attempt to deliver. A hook runs in Claude Code's
// critical path; it must give up rather than delay a turn.
const sendTimeout = 500 * time.Millisecond

// Payload is what a hook reports.
type Payload struct {
	Event          string `json:"event"`
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path"`
	PermissionMode string `json:"permission_mode"`

	// Pane is the identity this application gave the session when it started
	// it. It travels on the hook's own command line rather than in the
	// payload, because the payload's session id is Claude Code's and Claude
	// Code changes it: resuming a conversation writes a new transcript under a
	// new id, so the id we started with stops describing what is running.
	// This one never moves.
	Pane string `json:"pane,omitempty"`
}

// Listener accepts hook connections.
type Listener struct {
	ln   net.Listener
	path string
	ch   chan Payload
	done chan struct{}
}

// Listen opens a socket inside dir.
func Listen(dir string) (*Listener, error) {
	path := filepath.Join(dir, fmt.Sprintf("claudecontrol-%d.sock", os.Getpid()))
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("hooks: listen: %w", err)
	}
	l := &Listener{
		ln:   ln,
		path: path,
		ch:   make(chan Payload, 64),
		done: make(chan struct{}),
	}
	go l.accept()
	return l, nil
}

// Path is where hooks should send their payloads.
func (l *Listener) Path() string { return l.path }

// Events carries the payloads as they arrive.
func (l *Listener) Events() <-chan Payload { return l.ch }

func (l *Listener) accept() {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			return
		}
		go l.read(conn)
	}
}

// read takes one line from a connection. A payload that will not parse is
// dropped: a hook is spawned by Claude Code and must never be able to take the
// application down with a bad line.
func (l *Listener) read(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(sendTimeout))
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var p Payload
		if err := json.Unmarshal(sc.Bytes(), &p); err != nil {
			continue
		}
		select {
		case l.ch <- p:
		default: // the application is busy; a stale state beats a stall
		}
	}
}

// Close stops listening and removes the socket file.
func (l *Listener) Close() error {
	select {
	case <-l.done:
		return nil
	default:
		close(l.done)
	}
	err := l.ln.Close()
	_ = os.Remove(l.path)
	if err != nil {
		return fmt.Errorf("hooks: close: %w", err)
	}
	return nil
}

// Send delivers one payload. It is what --hook mode calls.
func Send(socket, event, pane string, stdin io.Reader) error {
	body, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return fmt.Errorf("hooks: read payload: %w", err)
	}
	var p Payload
	// An unreadable payload still reports the event: knowing that a session
	// moved is worth more than knowing nothing because a field changed shape.
	_ = json.Unmarshal(body, &p)
	p.Event = event
	p.Pane = pane

	line, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("hooks: encode payload: %w", err)
	}
	conn, err := net.DialTimeout("unix", socket, sendTimeout)
	if err != nil {
		return fmt.Errorf("hooks: dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(sendTimeout))
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("hooks: write: %w", err)
	}
	return nil
}

// hookEvents are the events worth a round trip. Anything else is left alone.
var hookEvents = []string{
	"UserPromptSubmit", "PreToolUse", "PostToolUse",
	"Notification", "Stop", "SessionEnd",
}

// SettingsJSON builds the value for --settings.
//
// It carries hooks and nothing else. --settings merges with the user's own
// configuration rather than replacing it, so anything extra here would quietly
// override a real preference.
func SettingsJSON(binary, socket, pane string) (string, error) {
	type cmd struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type group struct {
		Hooks []cmd `json:"hooks"`
	}

	registered := make(map[string][]group, len(hookEvents))
	for _, e := range hookEvents {
		registered[e] = []group{{Hooks: []cmd{{
			Type:    "command",
			Command: fmt.Sprintf("%s --hook %s --pane %s", binary, e, pane),
		}}}}
	}
	b, err := json.Marshal(map[string]any{"hooks": registered})
	if err != nil {
		return "", fmt.Errorf("hooks: encode settings: %w", err)
	}
	_ = socket // the socket travels by environment variable, not by argument
	return string(b), nil
}
