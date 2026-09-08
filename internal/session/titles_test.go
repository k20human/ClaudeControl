package session

import (
	"bytes"
	"strings"
	"testing"
)

// The title Claude Code sets, with the glyph that breaks the parser
// underneath: ✳ is E2 9C B3, and 0x9C is the C1 string terminator.
const (
	claudeTitle  = "\x1b]2;✳ Cat-543 rebase sur preprod\x07"
	cleanedTitle = "\x1b]2; Cat-543 rebase sur preprod\x07"
)

func filtered(t *testing.T, chunks ...string) string {
	t.Helper()
	var f titles
	var got bytes.Buffer
	for _, c := range chunks {
		got.Write(f.keep([]byte(c)))
	}
	return got.String()
}

// The title still arrives — the emulator learns what the window is called —
// without the character that would end the sequence in the middle of itself.
func TestATitleArrivesWithoutWhatBreaksTheParser(t *testing.T) {
	got := filtered(t, "before"+claudeTitle+"after")
	if got != "before"+cleanedTitle+"after" {
		t.Errorf("kept %q, want %q", got, "before"+cleanedTitle+"after")
	}
	// And what matters most: no part of it is left to be printed.
	if strings.Contains(got, "\x9c") {
		t.Errorf("the byte that terminates a string is still in there: %q", got)
	}
}

// Both endings, and the icon-name commands that carry the same prose. One
// ending is handed on, the one everything sends.
func TestEveryFormOfTitleIsCleaned(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"\x1b]2;✳ subject\x07", "\x1b]2; subject\x07"},
		{"\x1b]2;✳ subject\x1b\\", "\x1b]2; subject\x07"},
		{"\x1b]0;✽ subject\x07", "\x1b]0; subject\x07"},
		{"\x1b]1;icon name\x07", "\x1b]1;icon name\x07"},
		// Ü is C3 9C: the same trap in a word rather than a glyph.
		{"\x1b]2;Übersicht\x07", "\x1b]2;bersicht\x07"},
	} {
		if got := filtered(t, "a"+tc.in+"b"); got != "a"+tc.want+"b" {
			t.Errorf("%q became %q, want %q", tc.in, got, "a"+tc.want+"b")
		}
	}
}

// A sequence arrives split as often as not, and the split lands anywhere. The
// result has to be the same wherever it lands — which is the whole reason the
// state is kept between reads.
func TestASplitTitleIsCleanedTheSameWay(t *testing.T) {
	whole := "before" + claudeTitle + "after"
	want := filtered(t, whole)
	for at := 1; at < len(whole); at++ {
		if got := filtered(t, whole[:at], whole[at:]); got != want {
			t.Fatalf("split at %d gave %q, want %q", at, got, want)
		}
	}
}

// Everything else passes through byte for byte. The clipboard is the one that
// would be missed: a guest that copies would copy nothing.
func TestOtherSequencesPassThrough(t *testing.T) {
	for _, seq := range []string{
		"\x1b]52;c;SGVsbG8=\x07",                          // clipboard
		"\x1b]8;;https://example.com\x07link\x1b]8;;\x07", // hyperlink
		"\x1b]11;?\x07",                                   // background query
		"\x1b[31mred\x1b[0m",                              // colour
		"\x1b[2J\x1b[H",                                   // clear and home
		"plain text with an é and a ✳ in it",              // the glyph outside a sequence
		"\x9c",                                            // a bare string terminator
		"\x1b\x1b[A",                                      // two escapes in a row
	} {
		if got := filtered(t, seq); got != seq {
			t.Errorf("%q came through as %q", seq, got)
		}
	}
}

// A bare C1 terminator ends a title, the way the parser would have ended it.
// Reading past it would swallow the output behind it.
func TestABareTerminatorEndsATitle(t *testing.T) {
	got := filtered(t, "\x1b]2;subject\x9cand then real output")
	if want := "\x1b]2;subject\x07and then real output"; got != want {
		t.Errorf("kept %q, want %q", got, want)
	}
}

// A title that never ends must not swallow the output behind it for ever.
func TestATitleWithNoEndGivesUp(t *testing.T) {
	long := "\x1b]2;" + strings.Repeat("x", maxTitle+50) + "and then real output"
	got := filtered(t, long)
	if !strings.Contains(got, "real output") {
		t.Errorf("the output after an unterminated title was lost: %q", got)
	}
}

// A newline where a title's text should be says the sequence is malformed, and
// what follows is a guest drawing.
func TestAMalformedTitleReleasesTheOutput(t *testing.T) {
	got := filtered(t, "\x1b]2;half a title\nreal output")
	if !strings.Contains(got, "real output") {
		t.Errorf("output after a malformed title was lost: %q", got)
	}
}
