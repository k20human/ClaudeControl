package tabs

import (
	"os"
	"path/filepath"
	"strings"
)

// Where a tab opened at runtime starts.
//
// Not where the application was launched from, which is what it used to be and
// is rarely where you are working. A tab opened from a pane opens where that
// pane is working; a pane that says otherwise is obeyed; and a directory
// asked for by name beats both, because somebody said it out loud.

// withDir fills in the directory a new tab should open in, when the caller has
// not named one.
func (m *Module) withDir(opts map[string]any) map[string]any {
	if opts != nil {
		if v, ok := opts["dir"].(string); ok && strings.TrimSpace(v) != "" {
			return opts
		}
	}
	dir := m.defaultDir()
	if dir == "" {
		return opts
	}
	out := make(map[string]any, len(opts)+1)
	for k, v := range opts {
		out[k] = v
	}
	out["dir"] = dir
	return out
}

// Dir is where this pane is working, which is what it says when asked and
// where a pane opened beside it should open.
func (m *Module) Dir() string { return m.defaultDir() }

// defaultDir is what the pane says, or where the tab on screen is working.
func (m *Module) defaultDir() string {
	m.mu.Lock()
	dir := m.dir
	m.mu.Unlock()
	if dir != "" {
		return dir
	}
	if d, ok := m.Active().(interface{ Dir() string }); ok {
		return d.Dir()
	}
	return ""
}

// expandDir reads ~ the way a shell does, so a configuration can be written
// the way a person writes a path.
func expandDir(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}
