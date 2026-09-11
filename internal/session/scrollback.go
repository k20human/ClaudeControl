package session

import "sync/atomic"

// How much of what has scrolled off a pane is kept.
//
// Every session carries an emulator, and every emulator keeps the lines that
// have scrolled off the top so the wheel can go back to them. That memory is
// the largest thing this application holds, and it is not close: a cell is 112
// bytes — a rune, a style, a hyperlink — so a line of eighty columns costs
// about ten kilobytes once it is kept. Measured at 9.9 KB a line.
//
// The emulator's own default is ten thousand lines, which is 94 MiB a session
// with the history full. A window holding three conversations and six services
// therefore reaches a gigabyte and stays there, which is what it did.
//
// Two thousand lines is forty screens of scrolling and a fifth of the memory.
// It is a setting because the right answer depends on what you scroll back
// for; the arithmetic above is what to reason with.
const DefaultScrollback = 2000

// scrollback is the default every session starts with, set once from the
// configuration before any session exists. Atomic because it is read by
// whichever goroutine starts a session, which is not the one that set it.
var scrollback atomic.Int64

func init() { scrollback.Store(DefaultScrollback) }

// SetDefaultScrollback settles how many lines a session keeps. A value below
// zero is ignored; zero asks for nothing kept, which somebody who never
// scrolls back is entitled to want.
func SetDefaultScrollback(lines int) {
	if lines < 0 {
		return
	}
	scrollback.Store(int64(lines))
}

// scrollbackFor is what a spec asked for, or the default when it asked for
// nothing — never zero.
//
// Zero is the one number that cannot be passed on: the emulator reads it as
// "use mine", and mine is ten thousand lines. Asking for nothing would
// therefore have asked for the most there is. One line is what nothing is
// worth here, and it is 10 KB rather than 94 MiB.
func scrollbackFor(want int) int {
	if want <= 0 {
		want = int(scrollback.Load())
	}
	if want < 1 {
		return 1
	}
	return want
}
