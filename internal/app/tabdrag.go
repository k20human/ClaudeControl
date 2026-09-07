package app

import (
	"image/color"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/config"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/render"
)

// Moving a tab with the pointer.
//
// A press on a tab's label arms a move without committing to one: pressing a
// tab is also how a tab is selected, and the two cannot be told apart until
// the pointer moves. Once it does, the move is real and the drop preview
// follows — the same preview a pane drag draws, because the drop zones are the
// same and one grammar is enough.

var (
	tabDropFill = color.RGBA{R: 0x1e, G: 0x3f, B: 0x46, A: 0xff}
	tabDropEdge = color.RGBA{R: 0x4d, G: 0xd0, B: 0xe1, A: 0xff}
)

// tabDragState is the tab in flight.
type tabDragState struct {
	from  layout.PaneID
	index int
	title string

	// moved says the pointer left the cell it was pressed in. Until it does
	// this is a selection, not a move.
	moved bool

	target layout.PaneID
	side   layout.Side
}

// tabber is a pane that can give a tab up and take one in.
type tabber interface {
	TabAt(x, y int) (int, bool)
	Detach(int) (module.Module, module.Held, bool)
	Adopt(module.Module, module.Held) error
	Reorder(int, int)
	Count() int
}

// paneTabs is the pane's module if it is one that holds tabs.
func (a *App) paneTabs(id layout.PaneID) (tabber, bool) {
	m, ok := a.modules[id]
	if !ok {
		return nil, false
	}
	t, ok := m.(tabber)
	return t, ok
}

// armTabDrag notes that a press landed on a tab. The press is still forwarded:
// the tab becomes the one on screen straight away, so what you are dragging is
// what you are looking at.
func (a *App) armTabDrag(id layout.PaneID, localX, localY int) {
	t, ok := a.paneTabs(id)
	if !ok {
		return
	}
	i, ok := t.TabAt(localX, localY)
	if !ok {
		return
	}
	if a.secondClickOnTab(id, i) {
		// A double-click on a label names it, the way it does on the tabs of
		// an editor. The press has already selected the tab, so the prompt
		// opens on the one you are looking at.
		a.beginRenameTabAt(id, i)
		return
	}
	a.tabDrag = &tabDragState{from: id, index: i, target: id, side: layout.SideSwap}
}

// tabPress is the last press on a label, which is all a double-click needs to
// be recognised.
type tabPress struct {
	pane  layout.PaneID
	index int
	at    time.Time
}

// doubleClick is how long a second press on the same label still counts as
// part of the first. Terminals do not report double-clicks, so this is the
// application's own judgement of one.
const doubleClick = 400 * time.Millisecond

// secondClickOnTab records this press and reports whether it completes a
// double-click on the same label.
func (a *App) secondClickOnTab(id layout.PaneID, i int) bool {
	now := time.Now()
	was := a.lastTabPress
	a.lastTabPress = tabPress{pane: id, index: i, at: now}
	return was.pane == id && was.index == i && now.Sub(was.at) < doubleClick
}

// updateTabDrag follows the pointer, and is what turns a press into a move.
func (a *App) updateTabDrag(x, y int) {
	d := a.tabDrag
	if d == nil {
		return
	}
	d.moved = true
	if id := paneAt(a.rects, x, y); id != 0 {
		d.target = id
		d.side = dropZone(a.rects[id], x, y)
		// Over the strip of the pane it came from: this is a reordering, and
		// the whole-pane preview would be nonsense.
		if id == d.from && a.overStrip(id, x, y) {
			d.side = layout.SideSwap
		}
	}
	a.Wake()
}

// onStripRow reports whether a point is on a pane's tab strip at all —
// a tab, the cross, the plus or the space between them.
//
// The whole strip is chrome. It is what lets a click reach it on a pane that
// does not have the focus: the swallowed first click protects what is inside a
// pane from a stray gesture, and a strip is not inside it.
func (a *App) onStripRow(id layout.PaneID, y int) bool {
	if _, ok := a.paneTabs(id); !ok {
		return false
	}
	return y == a.contentRect(id).Y
}

// overStrip reports whether a point is on a pane's tab strip.
func (a *App) overStrip(id layout.PaneID, x, y int) bool {
	r := a.contentRect(id)
	if y != r.Y {
		return false
	}
	t, ok := a.paneTabs(id)
	if !ok {
		return false
	}
	_, on := t.TabAt(x-r.X, 0)
	return on
}

// finishTabDrag puts the tab where the preview said it would go.
func (a *App) finishTabDrag() {
	d := a.tabDrag
	a.tabDrag = nil
	if d == nil {
		return
	}
	a.clearNext = true
	if !d.moved {
		// It never left the label. That was a click, and the pane has already
		// treated it as one.
		return
	}

	switch {
	case d.target == d.from && a.overStrip(d.from, a.pointerX, a.pointerY):
		a.reorderTab(d)
	case d.target == d.from:
		// Dropped on its own pane, away from the strip. Nothing to do, and
		// nothing to explain: the tab is where it was.
	case d.side == layout.SideSwap:
		a.moveTabInto(d)
	default:
		a.promoteTab(d)
	}
	a.Wake()
}

// cancelTabDrag lets go of the tab where it started.
func (a *App) cancelTabDrag() {
	if a.tabDrag == nil {
		return
	}
	a.tabDrag = nil
	a.clearNext = true
	a.Wake()
}

// reorderTab moves a tab along its own strip.
func (a *App) reorderTab(d *tabDragState) {
	t, ok := a.paneTabs(d.from)
	if !ok {
		return
	}
	r := a.contentRect(d.from)
	to, on := t.TabAt(a.pointerX-r.X, 0)
	if !on || to == d.index {
		return
	}
	t.Reorder(d.index, to)
}

// moveTabInto drops a tab in the middle of another pane.
//
// A pane of tabs takes it. A pane that is not one is wrapped in a pane of tabs
// holding both — the middle means "with that" wherever it is dropped, and an
// exception for plain panes would be one more rule to remember for no gain.
func (a *App) moveTabInto(d *tabDragState) {
	if _, ok := a.paneTabs(d.target); !ok {
		if !a.wrapInTabs(d.target) {
			a.setStatus("that pane cannot take a tab")
			return
		}
	}
	dst, ok := a.paneTabs(d.target)
	if !ok {
		return
	}
	mod, h, ok := a.takeTab(d)
	if !ok {
		return
	}
	if err := dst.Adopt(mod, h); err != nil {
		a.setStatus("%s", err)
		a.putTabBack(d, mod, h)
		return
	}
	a.afterTabLeft(d)
	a.setFocus(d.target)
}

// promoteTab makes a tab a pane of its own, on the side the preview showed.
func (a *App) promoteTab(d *tabDragState) {
	mod, h, ok := a.takeTab(d)
	if !ok {
		return
	}
	a.nextPane++
	id := a.nextPane
	leaf := &layout.Node{Kind: layout.KindLeaf, PaneID: id}

	root, err := layout.SplitSide(a.root, d.target, leaf, d.side)
	if err != nil {
		a.setStatus("%s", err)
		a.putTabBack(d, mod, h)
		a.nextPane--
		return
	}
	a.root = root
	a.modules[id] = mod
	name := h.Name
	if name == "" {
		name = "term"
	}
	a.moduleNames[id] = name
	// Recorded as what it was built from, so a layout written later says what
	// this pane holds rather than only that it holds something. What it was
	// built with unless it can say better itself.
	opts := h.Options
	if live := valuesOf(mod); live != nil {
		opts = live
	}
	a.paneSpecs[id] = config.PaneSpec{Module: name, Options: opts}
	a.zoomed = 0
	a.layoutChanged = true

	a.afterTabLeft(d)
	a.setFocus(id)
}

// takeTab removes the tab being dragged from the pane it came from, and says
// what it was built from so a pane made out of it can be recorded.
func (a *App) takeTab(d *tabDragState) (mod module.Module, h module.Held, ok bool) {
	src, held := a.paneTabs(d.from)
	if !held {
		return nil, module.Held{}, false
	}
	mod, h, ok = src.Detach(d.index)
	if !ok {
		a.setStatus("that tab is no longer there")
		return nil, module.Held{}, false
	}
	if h.Title == "" {
		h.Title = d.title
	}
	return mod, h, true
}

// putTabBack returns a tab whose move fell through, so a failed drop costs
// nothing rather than a session.
func (a *App) putTabBack(d *tabDragState, mod module.Module, h module.Held) {
	if src, ok := a.paneTabs(d.from); ok {
		if err := src.Adopt(mod, h); err == nil {
			return
		}
	}
	_ = mod.Close()
}

// afterTabLeft removes the pane a tab came from if that was its last one.
//
// What is left behind is nothing, and a pane drawing nothing is not worth
// keeping: its share goes back to its neighbours, exactly as closing a pane
// gives it back.
func (a *App) afterTabLeft(d *tabDragState) {
	src, ok := a.paneTabs(d.from)
	if !ok || src.Count() > 0 {
		a.relayout()
		return
	}
	root, err := layout.Remove(a.root, d.from)
	if err != nil {
		// The last pane there is. It keeps its empty strip rather than
		// leaving the application with nothing at all to draw.
		a.relayout()
		return
	}
	if m, held := a.modules[d.from]; held {
		_ = m.Close()
	}
	delete(a.modules, d.from)
	delete(a.moduleNames, d.from)
	delete(a.paneSpecs, d.from)
	a.root = root
	a.zoomed = 0
	a.layoutChanged = true
	a.relayout()
}

// wrapInTabs turns a pane into a pane of tabs holding what it held.
func (a *App) wrapInTabs(id layout.PaneID) bool {
	held, ok := a.modules[id]
	if !ok {
		return false
	}
	wrapper, err := module.New("tabs", map[string]any{"tabs": []any{}})
	if err != nil {
		a.setStatus("%s", err)
		return false
	}
	if err := wrapper.Init(a.moduleContext(id)); err != nil {
		_ = wrapper.Close()
		a.setStatus("%s", err)
		return false
	}
	t, ok := wrapper.(tabber)
	if !ok {
		_ = wrapper.Close()
		return false
	}
	title := a.moduleNames[id]
	if title == "" {
		title = "pane"
	}
	if err := t.Adopt(held, module.Held{
		Title: title, Name: a.moduleNames[id], Options: a.paneSpecs[id].Options,
	}); err != nil {
		_ = wrapper.Close()
		a.setStatus("%s", err)
		return false
	}
	a.modules[id] = wrapper
	a.moduleNames[id] = "tabs"
	// A pane of tabs describes itself, so nothing has to be remembered for it
	// beyond what it is.
	a.paneSpecs[id] = config.PaneSpec{Module: "tabs"}
	a.relayout()
	return true
}

// drawTabDrag shows where the tab would land.
func (a *App) drawTabDrag(scr uv.Screen) {
	d := a.tabDrag
	if d == nil || !d.moved {
		return
	}
	r, ok := a.rects[d.target]
	if !ok {
		return
	}
	// Reordering shows nothing: the strip is already telling you where the
	// tab is, and tinting the pane under it would say something else.
	if d.target == d.from && a.overStrip(d.from, a.pointerX, a.pointerY) {
		return
	}
	if d.target == d.from {
		return
	}
	area := previewRect(r, d.side)
	label := d.side.String()
	if d.side == layout.SideSwap {
		label = "as a tab"
	}
	render.Fill(scr, uv.Rect(area.X, area.Y, area.W, area.H), tabDropFill)
	if area.H > 0 && area.W > 2 {
		render.Text(scr, area.X+1, area.Y+area.H/2, "▸ "+label, tabDropEdge, tabDropFill)
	}
}

// valuesOf is what a module says it holds, or nothing when it cannot say.
func valuesOf(mod module.Module) map[string]any {
	if v, ok := mod.(interface{ Values() map[string]any }); ok {
		return v.Values()
	}
	return nil
}
