package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Place is a directory you have worked in, and when you last did.
type Place struct {
	Dir  string
	Last time.Time
}

// headLines is how far into a transcript to look for the directory it ran in.
// The first record usually carries it; a few more cover the ones that open
// with something else.
const headLines = 40

// Places lists the directories you have had conversations in, most recent
// first.
//
// Read rather than decoded. Claude Code names a project folder after the
// directory, with the separators replaced — and a directory whose own name
// contains a dash cannot be recovered from that. So the newest transcript in
// each folder is opened and its first record is asked where it ran, which is
// exact and costs a few dozen small reads.
//
// A directory that has since been deleted is dropped: it is not somewhere to
// open anything.
func Places(limit int) []Place {
	root := ProjectsDir()
	if root == "" {
		return nil
	}
	folders, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	seen := make(map[string]time.Time, len(folders))
	for _, f := range folders {
		if !f.IsDir() {
			continue
		}
		path, at := newestTranscript(filepath.Join(root, f.Name()))
		if path == "" {
			continue
		}
		dir := ranIn(path)
		if dir == "" {
			continue
		}
		if was, ok := seen[dir]; !ok || at.After(was) {
			seen[dir] = at
		}
	}

	out := make([]Place, 0, len(seen))
	for dir, at := range seen {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, Place{Dir: dir, Last: at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// newestTranscript is the most recently written conversation in a folder.
func newestTranscript(folder string) (string, time.Time) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return "", time.Time{}
	}
	var path string
	var at time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if path == "" || fi.ModTime().After(at) {
			path, at = filepath.Join(folder, e.Name()), fi.ModTime()
		}
	}
	return path, at
}

// ranIn is the working directory a conversation ran in, from the first record
// that says.
func ranIn(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for i := 0; i < headLines && sc.Scan(); i++ {
		var rec struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Cwd != "" {
			return rec.Cwd
		}
	}
	return ""
}
