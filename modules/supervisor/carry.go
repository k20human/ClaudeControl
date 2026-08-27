package supervisor

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"

	"claudecontrol/internal/session"
)

// carryLines is how much of a run is kept for the next one. Enough to hold the
// stack trace that made you restart it, short enough that a service which
// prints a line a second does not carry a day of them around.
const carryLines = 400

// LogDir is where a service's last output is kept between runs.
//
// Beside the layout snapshot and for the same reason: this belongs to the
// machine, not to the person editing the configuration file.
func LogDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "services"
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "claudecontrol", "services")
}

// logName turns a service name into a file name. A service may be called
// anything, including something with a slash in it.
//
// The readable part is what survives the substitution; the four hex digits are
// of the name as written. Without them "api/v1" and "api-v1" would share a
// file and silently overwrite each other's record.
func logName(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		}
		return '-'
	}, name)
	if safe == "" || strings.Trim(safe, ".-") == "" {
		safe = "service"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return fmt.Sprintf("%s-%04x.log", safe, h.Sum32()&0xffff)
}

// textOf reads what a session has printed, history and screen together, as
// plain lines with the trailing blank ones dropped.
//
// Plain text, not the bytes the process wrote: the colours and the cursor
// moves are gone. What is being kept is the record of what happened, and a
// record that could still move the cursor is not a record.
func textOf(s *session.Session, keep int) string {
	if s == nil {
		return ""
	}
	total := s.History() + s.Rows()
	first := total - keep
	if first < 0 {
		first = 0
	}
	lines := make([]string, 0, total-first)
	for i := first; i < total; i++ {
		lines = append(lines, strings.TrimRight(s.LineText(i, 0, 1<<30), " "))
	}
	// Trailing blank lines are the unwritten part of the screen, not silence
	// from the process.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// captureLocked keeps what a service has printed so far, so that restarting it
// does not throw away the reason you restarted it.
func (m *Module) captureLocked(s *service) {
	if s.sess == nil {
		return
	}
	if text := textOf(s.sess, carryLines); text != "" {
		s.carry = text
	}
}

// replay puts the previous run back on the screen, under a rule saying where
// it ends. Called just after a session starts and before its process has had
// time to print, so the two never interleave.
func replay(sess *session.Session, carry string, cols int) {
	if sess == nil || carry == "" {
		return
	}
	rule := "── previous run ──"
	if pad := cols - len([]rune(rule)); pad > 0 {
		rule += strings.Repeat("─", pad)
	}
	// Dim, so that what is being read as history cannot be mistaken for what
	// the service is saying now.
	var b strings.Builder
	b.WriteString("\x1b[2m")
	b.WriteString(rule)
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(strings.TrimRight(carry, "\n"), "\n", "\r\n"))
	b.WriteString("\r\n")
	b.WriteString(strings.Repeat("─", max(1, cols)))
	b.WriteString("\x1b[0m\r\n")
	sess.Feed([]byte(b.String()))
}

// loadCarry reads what the last run of the application left behind.
func (m *Module) loadCarry() {
	dir := LogDir()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.svcs {
		raw, err := os.ReadFile(filepath.Join(dir, logName(s.spec.Name)))
		if err != nil {
			continue
		}
		s.carry = string(raw)
	}
}

// saveCarry writes each service's last output where the next run will find it.
//
// Best effort: a machine with no writable state directory still runs its
// services, and losing the record of a run is not worth refusing to quit over.
func (m *Module) saveCarry() {
	dir := LogDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	m.mu.Lock()
	type entry struct{ name, text string }
	out := make([]entry, 0, len(m.svcs))
	for _, s := range m.svcs {
		text := s.carry
		if s.sess != nil {
			if live := textOf(s.sess, carryLines); live != "" {
				text = live
			}
		}
		if text != "" {
			out = append(out, entry{s.spec.Name, text})
		}
	}
	m.mu.Unlock()

	for _, e := range out {
		_ = os.WriteFile(filepath.Join(dir, logName(e.name)), []byte(e.text), 0o600)
	}
}
