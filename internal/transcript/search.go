package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Hit is one conversation that contains what was asked for.
type Hit struct {
	SessionID string
	Title     string
	Dir       string
	Modified  time.Time
	Matches   int
	Excerpt   string
}

// maxLine is the longest transcript line that will be read. A line carrying a
// large file runs to megabytes, and a search has no business holding one in
// memory to look at forty characters of it.
const maxLine = 1 << 20

// Search finds the conversations in which something was said.
//
// Only what was said: a person's prompts and Claude's replies. Tool output,
// the contents of files that were read, and the machinery around them are
// skipped — a word that is common in code would otherwise match every
// conversation that ever opened a source file, which is the opposite of
// finding something.
//
// Case is ignored, accents included: "ÉLÉPHANT" is found by "éléphant" and
// the other way round. It is not accent-blind — "elephant" finds neither.
func Search(query string, limit int) []Hit {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	needle := []byte(strings.ToLower(query))

	files := transcripts()
	if len(files) == 0 {
		return nil
	}

	work := make(chan string)
	var mu sync.Mutex
	var hits []Hit

	var wg sync.WaitGroup
	workers := runtime.NumCPU()
	if workers > len(files) {
		workers = len(files)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				if h, ok := searchFile(path, needle); ok {
					mu.Lock()
					hits = append(hits, h)
					mu.Unlock()
				}
			}
		}()
	}
	for _, f := range files {
		work <- f
	}
	close(work)
	wg.Wait()

	// Newest first: you are usually looking for something recent, and a list
	// ordered by how often a word appears would put a long-ago conversation
	// that repeated it above the one you had this morning.
	sort.Slice(hits, func(i, j int) bool { return hits[i].Modified.After(hits[j].Modified) })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// transcripts lists every transcript on the machine.
func transcripts() []string {
	root := ProjectsDir()
	if root == "" {
		return nil
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".jsonl") {
				out = append(out, filepath.Join(root, d.Name(), f.Name()))
			}
		}
	}
	return out
}

// searchFile scans one conversation.
func searchFile(path string, needle []byte) (Hit, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Hit{}, false
	}
	defer f.Close()

	hit := Hit{SessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	if st, err := f.Stat(); err == nil {
		hit.Modified = st.ModTime()
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		line := sc.Bytes()
		// The title and the directory are worth having whether or not this
		// line matches, so they are read first and cheaply.
		if hit.Title == "" || hit.Dir == "" {
			readHeader(line, &hit)
		}
		// A cheap look at the raw line first: parsing every line of four
		// hundred megabytes to find a word in a few of them would be the
		// slowest possible way round.
		if !containsFold(line, needle) {
			continue
		}
		said := spokenText(line)
		if said == "" || !strings.Contains(strings.ToLower(said), string(needle)) {
			continue
		}
		hit.Matches++
		if hit.Excerpt == "" {
			hit.Excerpt = excerpt(said, string(needle))
		}
	}
	return hit, hit.Matches > 0
}

// readHeader picks the conversation's name and where it ran out of any line
// that carries them.
func readHeader(line []byte, hit *Hit) {
	var d struct {
		Type    string `json:"type"`
		AITitle string `json:"aiTitle"`
		Cwd     string `json:"cwd"`
	}
	if json.Unmarshal(line, &d) != nil {
		return
	}
	if d.AITitle != "" {
		hit.Title = d.AITitle
	}
	if d.Cwd != "" {
		hit.Dir = d.Cwd
	}
}

// spokenText is what a person or Claude actually said on this line, and empty
// for everything else.
//
// A person's prompt arrives as plain text. Claude's reply arrives as blocks,
// of which only the text ones were shown to you: thinking was not, a tool call
// is not speech, and a tool result is the machine talking to itself.
func spokenText(line []byte) string {
	var d struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal(line, &d) != nil {
		return ""
	}
	if d.Type != "user" && d.Type != "assistant" {
		return ""
	}
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(d.Message, &msg) != nil {
		return ""
	}

	var plain string
	if json.Unmarshal(msg.Content, &plain) == nil {
		return plain
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" && blk.Text != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

// containsFold is a case-insensitive search that allocates nothing, needle
// already lowered.
//
// Two paths, because they are correct under different conditions. An ASCII
// needle can be folded byte by byte: an ASCII letter never appears inside a
// multi-byte sequence, whose bytes are all above 0x7F. A needle with an
// accent in it cannot — É and é differ in the second byte of the pair, and a
// byte fold would miss "ÉLÉPHANT" for "éléphant", which is most of what a
// search in French is for. That one decodes runes instead, and pays for it
// only when the query asks.
func containsFold(haystack, needle []byte) bool {
	if !asciiOnly(needle) {
		return containsFoldRunes(haystack, needle)
	}
	return containsFoldASCII(haystack, needle)
}

// asciiOnly reports whether every byte is a plain ASCII one.
func asciiOnly(b []byte) bool {
	for _, c := range b {
		if c >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// containsFoldRunes is the same search a rune at a time, folding through
// unicode.ToLower so that every alphabet is folded the way its own case rules
// say, not the way ASCII's do.
func containsFoldRunes(haystack, needle []byte) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	// Decoded once, not once per position: this runs over every line of every
	// transcript, and the first rune is the whole of the cheap rejection.
	first, _ := utf8.DecodeRune(needle)
	for i := 0; i < len(haystack); {
		r, size := utf8.DecodeRune(haystack[i:])
		if unicode.ToLower(r) == first && matchesFrom(haystack[i:], needle) {
			return true
		}
		i += size
	}
	return false
}

// matchesFrom reports whether the needle runs from the start of haystack,
// folding each rune of the haystack as it goes.
func matchesFrom(haystack, needle []byte) bool {
	for n := 0; n < len(needle); {
		want, nsize := utf8.DecodeRune(needle[n:])
		if len(haystack) == 0 {
			return false
		}
		got, hsize := utf8.DecodeRune(haystack)
		if unicode.ToLower(got) != want {
			return false
		}
		haystack, n = haystack[hsize:], n+nsize
	}
	return true
}

func containsFoldASCII(haystack, needle []byte) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	last := len(haystack) - len(needle)
	for i := 0; i <= last; i++ {
		if lower(haystack[i]) != needle[0] {
			continue
		}
		j := 1
		for ; j < len(needle); j++ {
			if lower(haystack[i+j]) != needle[j] {
				break
			}
		}
		if j == len(needle) {
			return true
		}
	}
	return false
}

func lower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// excerptSpan is how much of a matching line is kept, in runes.
const excerptSpan = 110

// excerpt is the matching text with its surroundings, on one line.
func excerpt(said, needle string) string {
	flat := strings.Join(strings.Fields(said), " ")
	at := strings.Index(strings.ToLower(flat), needle)
	if at < 0 {
		at = 0
	}
	runes := []rune(flat)
	// Count in runes so a multi-byte letter is not cut in half.
	start := len([]rune(flat[:at])) - excerptSpan/3
	if start < 0 {
		start = 0
	}
	end := start + excerptSpan
	if end > len(runes) {
		end = len(runes)
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
