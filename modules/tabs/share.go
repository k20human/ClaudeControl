package tabs

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// How the strip is divided.
//
// The tabs fill it, the way a terminal's do. Compact labels left most of the
// row as bare background, which reads as a row of small buttons rather than as
// tabs — and made a tab a smaller target than the space it appeared to own.

// tabMin is the narrowest a tab may be squeezed to. Below it a label is more
// ellipsis than name, and a strip of those says less than a count of what does
// not fit.
const tabMin = 10

// fitLocked is how many tabs the strip can hold at tabMin, and how many are
// left over.
func (m *Module) fitLocked(width int) (shown, hidden int) {
	if len(m.tabs) == 0 || width <= 0 {
		return 0, len(m.tabs)
	}
	shown = width / tabMin
	// Always one, however narrow. A strip that hid its only tab left it
	// invisible and unpressable — and a tab you cannot press is one you
	// cannot move, close, or even choose.
	if shown < 1 {
		shown = 1
	}
	if shown > len(m.tabs) {
		shown = len(m.tabs)
	}
	return shown, len(m.tabs) - shown
}

// shareOut divides a width into n parts as evenly as it goes, the remainder
// spread over the first parts so that the whole width is used and no tab is a
// column narrower than its neighbour for no reason.
func shareOut(width, n int) []int {
	if n <= 0 {
		return nil
	}
	out := make([]int, n)
	base, extra := width/n, width%n
	for i := range out {
		out[i] = base
		if i < extra {
			out[i]++
		}
	}
	return out
}

// clipTab shortens a label to a width, keeping the beginning: a title's first
// words are the ones that tell it apart.
func clipTab(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// padTab fills a label out to its share, so the tab's colour covers the whole
// of what you can click.
func padTab(s string, w int) string {
	if pad := w - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// TabWidths is how wide each tab was drawn, pane-local. It exists so a test
// can check the strip was actually divided rather than take a screenshot's
// word for it.
func (m *Module) TabWidths() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]int, 0, len(m.tabs))
	for _, t := range m.tabs {
		out = append(out, t.w)
	}
	return out
}

// PlusColumn is where the + button starts, pane-local: everything before it
// belongs to the tabs.
func (m *Module) PlusColumn() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.plusX
}
