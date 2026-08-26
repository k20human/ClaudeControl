package app

import (
	"fmt"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/session"
)

// newPane splits the focused pane and starts a Claude Code session in the new
// half. Later stages will offer a chooser; stage 1 always opens claude.
func (a *App) newPane(o layout.Orientation) error {
	a.nextPane++
	id := a.nextPane

	m, err := module.New("claude", nil)
	if err != nil {
		return err
	}
	if err := m.Init(a.moduleContext(id)); err != nil {
		return err
	}

	root, err := layout.Split(a.root, a.focus, &layout.Node{Kind: layout.KindLeaf, PaneID: id}, o)
	if err != nil {
		_ = m.Close()
		return err
	}
	a.root = root
	a.modules[id] = m
	a.zoomed = 0
	a.relayout()
	a.setFocus(id)
	return nil
}

// closePane closes a pane's module and removes its leaf. Closing the last pane
// ends the application.
func (a *App) closePane(id layout.PaneID) error {
	m, ok := a.modules[id]
	if !ok {
		return fmt.Errorf("app: pane %d not found", id)
	}
	if err := m.Close(); err != nil {
		return err
	}
	delete(a.modules, id)

	root, err := layout.Remove(a.root, id)
	if err != nil {
		return err
	}
	a.root = root
	if root == nil {
		a.quit = true
		return nil
	}
	if a.zoomed == id {
		a.zoomed = 0
	}
	a.relayout()
	if _, still := a.rects[a.focus]; !still {
		if ids := layout.Leaves(a.root); len(ids) > 0 {
			a.focus = ids[0]
			a.prev = ids[0]
		}
	}
	return nil
}

// toggleZoom shows the focused pane full-area, or restores the layout. The
// tree is never modified, so unzooming restores it exactly.
func (a *App) toggleZoom() {
	if a.zoomed != 0 {
		a.zoomed = 0
	} else {
		a.zoomed = a.focus
	}
	a.relayout()
}

var exitedFg = color.RGBA{R: 0xe5, G: 0x93, B: 0x3a, A: 0xff}

// exitedBanner overwrites the bottom row of a dead pane. The pane content is
// left visible on purpose: when a session dies on an error, that error is
// exactly what has to stay readable.
func exitedBanner(scr uv.Screen, area uv.Rectangle, code int) {
	// Only advertise what is actually bound. A banner offering a key that does
	// nothing is worse than a banner offering none.
	text := fmt.Sprintf(" exited (%d) — alt+x to close ", code)
	y := area.Min.Y + area.Dy() - 1
	runes := []rune(text)
	for i := 0; i < area.Dx(); i++ {
		cell := uv.EmptyCell
		if i < len(runes) {
			cell.Content = string(runes[i])
		}
		cell.Style.Fg = exitedFg
		scr.SetCell(area.Min.X+i, y, &cell)
	}
}

// sessioner is implemented by modules that host a process.
type sessioner interface{ Session() *session.Session }

// exitedCode reports the exit code of a pane whose process is gone.
func (a *App) exitedCode(id layout.PaneID) (int, bool) {
	m, ok := a.modules[id].(sessioner)
	if !ok || m.Session() == nil {
		return 0, false
	}
	st, code := m.Session().Status()
	return code, st == session.Exited
}

// rotateFocusedSplit flips the split that holds the focused pane between side
// by side and stacked.
func (a *App) rotateFocusedSplit() {
	_, parent, _ := layout.Find(a.root, a.focus)
	if parent == nil || parent.Kind != layout.KindSplit {
		return
	}
	if parent.Orientation == layout.Horizontal {
		parent.Orientation = layout.Vertical
	} else {
		parent.Orientation = layout.Horizontal
	}
	a.relayout()
}

// evenOutSplits gives every child of every split the same share. It is the
// cheap way back from a layout that has been dragged into a corner.
func (a *App) evenOutSplits() {
	var walk func(n *layout.Node)
	walk = func(n *layout.Node) {
		if n == nil {
			return
		}
		if n.Kind == layout.KindSplit {
			for i := range n.Ratios {
				n.Ratios[i] = 1
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(a.root)
	a.relayout()
}
