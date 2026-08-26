package app

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/render"
	"claudecontrol/internal/session"
	"claudecontrol/modules/sessions"
)

// selector is the part of the sessions module the panel drives.
type selector interface {
	module.Module
	Selected() (session.ID, bool)
	MoveSelection(int)
	Entries() []*pool.Entry
}

var panelFooterFg = color.RGBA{R: 0x5d, G: 0x69, B: 0x7c, A: 0xff}

// toggleSessionPanel opens or closes the session list.
//
// The list is an overlay rather than a pane: it is something you consult and
// dismiss, and turning it into a pane would rearrange the layout every time.
func (a *App) toggleSessionPanel() {
	if a.sessionPanel != nil {
		_ = a.sessionPanel.Close()
		a.sessionPanel = nil
		a.clearNext = true
		return
	}
	m, err := sessions.New(nil)
	if err != nil {
		return
	}
	sel, ok := m.(selector)
	if !ok {
		return
	}
	if err := sel.Init(module.Context{
		PaneID: 0, Pool: a.pool, Bus: a.bus, Wake: a.Wake,
	}); err != nil {
		return
	}
	a.sessionPanel = sel
	a.clearNext = true
}

// sessionPanelRect is where the list is drawn: centred, and never over the
// status bar.
func (a *App) sessionPanelRect() layout.Rect {
	avail := a.paneArea()
	w, h := 64, 16
	if w > avail.W-4 {
		w = avail.W - 4
	}
	if h > avail.H-4 {
		h = avail.H - 4
	}
	if w < 20 {
		w = avail.W
	}
	if h < 5 {
		h = avail.H
	}
	return layout.Rect{
		X: avail.X + (avail.W-w)/2,
		Y: avail.Y + (avail.H-h)/2,
		W: w, H: h,
	}
}

// drawSessionPanel paints the list and its footer.
func (a *App) drawSessionPanel(scr uv.Screen) {
	if a.sessionPanel == nil {
		return
	}
	r := a.sessionPanelRect()
	render.Fill(scr, uv.Rect(r.X, r.Y, r.W, r.H), panelBg)
	render.Text(scr, r.X+2, r.Y+1, "SESSIONS", panelKey, panelBg)

	body := layout.Rect{X: r.X + 2, Y: r.Y + 3, W: r.W - 4, H: r.H - 5}
	if body.H > 0 && body.W > 0 {
		_ = a.sessionPanel.Resize(body.W, body.H)
		a.sessionPanel.Draw(scr, uv.Rect(body.X, body.Y, body.W, body.H))
	}
	render.Text(scr, r.X+2, r.Y+r.H-1,
		"enter attach   d detach   x kill   esc close",
		panelFooterFg, panelBg)
}

// attachSelected shows the highlighted session in the focused pane.
func (a *App) attachSelected() {
	if a.sessionPanel == nil {
		return
	}
	id, ok := a.sessionPanel.Selected()
	if !ok {
		return
	}
	e, found := a.pool.Get(id)
	if !found || e.Attached {
		return
	}
	a.pool.SetAttached(id, true)
	a.toggleSessionPanel()
}

// detachSelected takes the highlighted session off screen without ending it.
func (a *App) detachSelected() {
	if a.sessionPanel == nil {
		return
	}
	if id, ok := a.sessionPanel.Selected(); ok {
		a.pool.SetAttached(id, false)
	}
}

// killSelected ends the highlighted session for good.
func (a *App) killSelected() {
	if a.sessionPanel == nil {
		return
	}
	if id, ok := a.sessionPanel.Selected(); ok {
		_ = a.pool.Kill(id)
	}
}
