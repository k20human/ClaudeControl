package app

import (
	"errors"
	"image/color"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"claudecontrol/internal/clipboard"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/render"
)

var (
	menuBg    = color.RGBA{R: 0x1b, G: 0x21, B: 0x2b, A: 0xff}
	menuFg    = color.RGBA{R: 0x9a, G: 0xa8, B: 0xbd, A: 0xff}
	menuHint  = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}
	menuSelBg = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
	menuSelFg = color.RGBA{R: 0x10, G: 0x16, B: 0x1e, A: 0xff}
)

// menuItem is one line of the context menu.
type menuItem struct {
	label string
	hint  string
	run   func(*App)
}

// menuState is an open context menu.
type menuState struct {
	rect  layout.Rect
	items []menuItem
	sel   int
}

// menuItems is what right-clicking a pane offers.
//
// Paste comes first because it is the reason the menu exists: enabling mouse
// reporting takes the right button away from the terminal, and with it the
// menu the terminal would have shown. Having taken it, we owe one back.
func (a *App) menuItems() []menuItem {
	// The clipboard entries say what they will need before you press them.
	// Finding out afterwards, from a message about a copy that did not
	// happen, is finding out too late.
	clip := ""
	if _, ok := clipboard.Helper(); !ok {
		clip = "no clipboard tool"
	}
	return []menuItem{
		{"copy", clip, (*App).copySelection},
		{"paste", clip, (*App).pasteFromClipboard},
		{"find", "alt+:", (*App).toggleFind},
		{"new session", "alt+n", func(a *App) { _ = a.newPane(layout.Horizontal) }},
		{"close pane", "alt+x", func(a *App) { _ = a.closePane(a.focus) }},
		{"zoom", "alt+z", (*App).toggleZoom},
		{"move pane", "alt+r", func(a *App) { a.beginPaneDrag(a.focus) }},
	}
}

// openMenu puts a menu at the pointer, kept inside the screen.
func (a *App) openMenu(x, y int) {
	items := a.menuItems()
	w := 0
	for _, it := range items {
		if n := ansi.StringWidth(it.label) + ansi.StringWidth(it.hint) + 4; n > w {
			w = n
		}
	}
	h := len(items)

	// Below and right of the pointer by preference, flipped rather than
	// clipped when there is no room: half a menu is not a menu.
	if x+w > a.area.W {
		x = a.area.W - w
	}
	if y+h > a.area.H-1 {
		y = a.area.H - 1 - h
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	a.menu = &menuState{rect: layout.Rect{X: x, Y: y, W: w, H: h}, items: items}
	a.Wake()
}

func (a *App) closeMenu() {
	if a.menu == nil {
		return
	}
	a.menu = nil
	a.clearNext = true
	a.Wake()
}

// drawMenu paints the open menu.
func (a *App) drawMenu(scr uv.Screen) {
	if a.menu == nil {
		return
	}
	r := a.menu.rect
	render.Fill(scr, uv.Rect(r.X, r.Y, r.W, r.H), menuBg)
	for i, it := range a.menu.items {
		fg, bg := color.Color(menuFg), color.Color(menuBg)
		hint := color.Color(menuHint)
		if i == a.menu.sel {
			fg, bg, hint = menuSelFg, menuSelBg, menuSelFg
			render.Fill(scr, uv.Rect(r.X, r.Y+i, r.W, 1), menuSelBg)
		}
		render.Text(scr, r.X+1, r.Y+i, it.label, fg, bg)
		if it.hint != "" {
			render.Text(scr, r.X+r.W-1-ansi.StringWidth(it.hint), r.Y+i, it.hint, hint, bg)
		}
	}
}

// menuKey drives the menu, and reports whether it took the key.
func (a *App) menuKey(e uv.KeyPressEvent) bool {
	if a.menu == nil {
		return false
	}
	key := e.Key()
	switch {
	case key.Code == uv.KeyEscape:
		a.closeMenu()
	case key.Code == uv.KeyUp || key.Text == "k":
		a.menuMove(-1)
	case key.Code == uv.KeyDown || key.Text == "j":
		a.menuMove(1)
	case key.Code == uv.KeyEnter:
		a.menuChoose(a.menu.sel)
	default:
		// Anything else dismisses it and is not passed on: a menu that
		// swallowed a keystroke silently would lose what you typed.
		a.closeMenu()
	}
	return true
}

func (a *App) menuMove(d int) {
	a.menu.sel += d
	if a.menu.sel < 0 {
		a.menu.sel = len(a.menu.items) - 1
	}
	if a.menu.sel >= len(a.menu.items) {
		a.menu.sel = 0
	}
	a.Wake()
}

func (a *App) menuChoose(i int) {
	if i < 0 || i >= len(a.menu.items) {
		return
	}
	run := a.menu.items[i].run
	a.closeMenu()
	run(a)
}

// menuMouse handles a click while the menu is open, and reports whether it
// took the event.
func (a *App) menuMouse(ev uv.MouseEvent, m uv.Mouse) bool {
	if a.menu == nil {
		return false
	}
	r := a.menu.rect
	inside := m.X >= r.X && m.X < r.X+r.W && m.Y >= r.Y && m.Y < r.Y+r.H
	if _, isMotion := ev.(uv.MouseMotionEvent); isMotion {
		if inside {
			a.menu.sel = m.Y - r.Y
			a.Wake()
		}
		return true
	}
	if _, isClick := ev.(uv.MouseClickEvent); !isClick {
		return true
	}
	if !inside {
		// A click elsewhere dismisses without acting, which is what every
		// menu does and what fingers expect.
		a.closeMenu()
		return true
	}
	a.menuChoose(m.Y - r.Y)
	return true
}

// clipboardWait is how long the terminal is given to answer a clipboard
// request before the interface says it did not.
const clipboardWait = 600 * time.Millisecond

// pasteFromClipboard puts the system clipboard into the focused pane.
//
// There are two ways for a terminal application to reach the clipboard and
// neither always works. A helper program is asked first, because when one is
// installed it simply answers. Failing that the terminal itself is asked over
// OSC 52 — which many terminals refuse, since it lets any program read what
// you copied. If neither works the interface says so and names the remedy,
// rather than a paste that quietly does nothing.
func (a *App) pasteFromClipboard() {
	text, err := clipboard.Read(300 * time.Millisecond)
	switch {
	case err == nil && text == "":
		a.setStatus("the clipboard is empty")
	case err == nil:
		a.deliverPaste(text)
	case errors.Is(err, clipboard.ErrNoHelper):
		a.askTerminalForClipboard()
	default:
		a.setStatus("%s", err)
	}
}

// copySelection puts the focused pane's selection on the clipboard.
//
// The same two ways as reading, and the same order: a helper program if one is
// installed, and otherwise the terminal itself over OSC 52. Writing is the
// half terminals are likelier to allow, since handing text to the clipboard
// gives nothing away.
func (a *App) copySelection() {
	m, ok := a.modules[a.focus]
	if !ok {
		return
	}
	sel, ok := m.(interface{ SelectedText() string })
	if !ok {
		a.setStatus("nothing in this pane can be selected")
		return
	}
	text := sel.SelectedText()
	if text == "" {
		a.setStatus("nothing is selected — drag across the text first")
		return
	}

	switch err := clipboard.Write(text, 300*time.Millisecond); {
	case err == nil:
		a.setStatus("copied %d characters", len([]rune(text)))
	case errors.Is(err, clipboard.ErrNoHelper):
		// No helper, so ask the terminal to take it. There is no reply and no
		// way to know whether it did, and most terminals refuse — handing
		// text to the clipboard is how a program could overwrite what you
		// copied. The message must not read as a success.
		_, _ = a.term.Write([]byte(ansi.SetClipboard(ansi.SystemClipboard, text)))
		// The remedy first: the bar truncates, and what is cut has to be the
		// part you could have guessed. It stays conditional because a
		// terminal that does accept OSC 52 has just taken the text.
		a.setStatus("install wl-clipboard if the copy did not take")
	default:
		a.setStatus("%s", err)
	}
}

// askTerminalForClipboard sends the OSC 52 query. The reply, if it comes,
// arrives as a ClipboardEvent like any other input.
func (a *App) askTerminalForClipboard() {
	a.clipboardAsked = time.Now()
	_, _ = a.term.Write([]byte(ansi.RequestSystemClipboard))
	a.setStatus("install wl-clipboard if nothing pastes")
	a.Wake()
}

// clipboardTimedOut is called each frame and reports the silence once.
func (a *App) clipboardTimedOut() {
	if a.clipboardAsked.IsZero() || time.Since(a.clipboardAsked) < clipboardWait {
		return
	}
	a.clipboardAsked = time.Time{}
	a.setStatus("the terminal refused — paste with ctrl+shift+v")
}

// deliverPaste hands text to the focused module, whole.
func (a *App) deliverPaste(text string) {
	m, ok := a.modules[a.focus]
	if !ok {
		return
	}
	in, ok := m.(module.Inputter)
	if !ok {
		return
	}
	in.Paste(text)
	a.Wake()
}
