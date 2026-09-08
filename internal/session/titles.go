package session

import "bytes"

// Making the window title a guest sets safe to hand to the emulator.
//
// A guest names the terminal window, and Claude Code does it several times a
// turn: the subject of the conversation, with a glyph in front of it. That
// sequence cannot be passed on as it stands.
//
// The parser underneath allows UTF-8 inside an OSC string — every byte from
// 0x20 to 0xFF is text there — and then makes one exception, for 0x9C, the C1
// string terminator. But 0x9C is a byte of ordinary characters: ✳ is E2 9C B3,
// and so are ✽ and ✻, the glyphs Claude Code puts in front of a title; Ü is
// C3 9C. The string therefore ends in the middle of a character, and the rest
// of the title is printed into the pane, over whatever the guest had drawn
// there. What that looks like is the name of the conversation lying across its
// own prompt.
//
// So the title is collected here and handed on without the characters that
// cannot survive the trip. The emulator still learns it — a host that wants to
// show a guest's title can — and none of it reaches the screen. Only OSC 0, 1
// and 2 are touched, the three that carry prose; the clipboard, hyperlinks and
// colour queries pass byte for byte.

// titles is the state of that cleaning, kept between reads because a sequence
// arrives split as often as not.
type titles struct {
	state int

	// intro is "ESC ] 2 ;", held while the sequence is still undecided and
	// reused as the head of the one handed on.
	intro []byte
	// body is the title so far, cleaned.
	body []byte
	// char is the character being decoded, which may be split across reads,
	// and need is how many of its bytes are still to come.
	char []byte
	need int

	// out is reused: this runs on every read from every session.
	out []byte
}

const (
	atGround = iota
	afterEsc
	inIntro
	inTitle
	inTitleEsc
)

const (
	// ESC, ] and up to five digits. Nothing legitimate is longer.
	maxIntro = 8
	// A title is a line, not a document. Past this the sequence is malformed,
	// and what follows it matters more than finishing it.
	maxTitle = 8 << 10
	// The C1 string terminator, and the byte that makes all this necessary.
	c1ST = 0x9c
)

// keep returns the bytes of a read for the emulator to see.
//
// The slice is reused and is only valid until the next call, which is how it
// is used: written to the emulator and forgotten.
func (t *titles) keep(in []byte) []byte {
	t.out = t.out[:0]
	for _, b := range in {
		switch t.state {
		case atGround:
			if b == 0x1b {
				t.state = afterEsc
				continue
			}
			t.out = append(t.out, b)

		case afterEsc:
			switch b {
			case ']':
				t.state = inIntro
				t.intro = append(t.intro[:0], 0x1b, ']')
			case 0x1b:
				// Two in a row: the first began nothing.
				t.out = append(t.out, 0x1b)
			default:
				t.state = atGround
				t.out = append(t.out, 0x1b, b)
			}

		case inIntro:
			switch {
			case b >= '0' && b <= '9' && len(t.intro) < maxIntro:
				t.intro = append(t.intro, b)
			case b == ';' && isTitleCommand(t.intro[2:]):
				t.state = inTitle
				t.intro = append(t.intro, ';')
				t.body, t.char, t.need = t.body[:0], t.char[:0], 0
			default:
				t.state = atGround
				t.out = append(t.out, t.intro...)
				t.out = append(t.out, b)
			}

		case inTitle:
			t.text(b)

		case inTitleEsc:
			// ESC \ ends the title; anything else began something in the
			// middle of one, which is malformed either way.
			t.state = atGround
			t.emit()
			if b != '\\' {
				t.out = append(t.out, 0x1b, b)
			}
		}
	}
	return t.out
}

// text takes one byte of a title's text.
func (t *titles) text(b byte) {
	if t.need > 0 {
		if b >= 0x80 && b <= 0xbf {
			t.char = append(t.char, b)
			t.need--
			if t.need == 0 {
				t.character()
			}
			return
		}
		// A character that ended early. What was collected is not one, and
		// the byte in hand is something else.
		t.char, t.need = t.char[:0], 0
	}

	switch {
	case b == 0x07, b == c1ST:
		t.emit()
		t.state = atGround
	case b == 0x1b:
		t.state = inTitleEsc
	case b < 0x20:
		// A control character where the text should be: the sequence is
		// malformed, and what follows is the guest drawing rather than naming.
		t.emit()
		t.state = atGround
		t.out = append(t.out, b)
	case len(t.body) > maxTitle:
		// A title with no end is not a name. Dropped whole.
		t.state = atGround
		t.body = t.body[:0]
	case b < 0x80:
		t.body = append(t.body, b)
	default:
		t.need = continuations(b)
		t.char = append(t.char[:0], b)
		if t.need == 0 {
			t.character()
		}
	}
}

// character adds the character just decoded, unless it is one the parser
// cannot carry.
func (t *titles) character() {
	if bytes.IndexByte(t.char, c1ST) < 0 {
		t.body = append(t.body, t.char...)
	}
	t.char = t.char[:0]
}

// emit hands the collected title on, ended with BEL: one ending is enough,
// and it is the one everything sends.
func (t *titles) emit() {
	t.out = append(t.out, t.intro...)
	t.out = append(t.out, t.body...)
	t.out = append(t.out, 0x07)
	t.body, t.char, t.need = t.body[:0], t.char[:0], 0
}

// continuations is how many bytes follow a lead byte, and zero for a byte that
// leads nothing.
func continuations(b byte) int {
	switch {
	case b >= 0xf0:
		return 3
	case b >= 0xe0:
		return 2
	case b >= 0xc0:
		return 1
	}
	return 0
}

// isTitleCommand reports whether an OSC number names the window or the icon.
// Nothing else is touched.
func isTitleCommand(num []byte) bool {
	if len(num) != 1 {
		return false
	}
	return num[0] == '0' || num[0] == '1' || num[0] == '2'
}
