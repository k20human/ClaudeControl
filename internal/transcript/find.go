package transcript

import (
	"os"
	"path/filepath"
)

// ProjectsDir is where Claude Code keeps one directory of transcripts per
// working directory. The same CLAUDE_CONFIG_DIR override Claude Code honours
// applies, so a relocated configuration is found rather than reported missing.
func ProjectsDir() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "projects")
}

// Find returns a session's transcript, and whether it has one.
//
// The directories are named after the working directory the session ran in,
// which this does not try to reproduce: the file is named after the session,
// so looking for that name under every project is both simpler and right when
// a session has moved.
func Find(sessionID string) (string, bool) {
	if sessionID == "" {
		return "", false
	}
	root := ProjectsDir()
	if root == "" {
		return "", false
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name(), sessionID+".jsonl")
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path, true
		}
	}
	return "", false
}

// Exists reports whether a session has a transcript, which is what makes it
// resumable: `claude --resume` on a session that never said anything fails
// with an error there is nothing useful to do about.
func Exists(sessionID string) bool {
	_, ok := Find(sessionID)
	return ok
}
