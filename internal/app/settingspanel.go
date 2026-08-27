package app

import (
	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/module"
	stg "claudecontrol/internal/settings"
	panel "claudecontrol/modules/settings"
)

// toggleSettingsPanel opens or closes the menu for the focused pane.
//
// It is built fresh each time from what the module publishes, so a module that
// gained a setting since the last time shows it without any bookkeeping.
func (a *App) toggleSettingsPanel() {
	if a.settingsPanel != nil {
		a.settingsPanel = nil
		a.clearNext = true
		return
	}
	m, ok := a.modules[a.focus]
	if !ok {
		return
	}
	p, ok := m.(module.Provider)
	if !ok {
		a.setStatus("this pane has nothing to configure")
		return
	}
	items := p.Settings()
	if len(items) == 0 {
		a.setStatus("this pane has nothing to configure")
		return
	}
	a.settingsPanel = panel.NewPanel(items)
	a.clearNext = true
}

// settingsPanelRect is where the menu is drawn.
func (a *App) settingsPanelRect() layout.Rect {
	avail := a.paneArea()
	w, h := 64, 16
	if w > avail.W-4 {
		w = avail.W - 4
	}
	if h > avail.H-4 {
		h = avail.H - 4
	}
	if w < 24 {
		w = avail.W
	}
	if h < 6 {
		h = avail.H
	}
	return layout.Rect{
		X: avail.X + (avail.W-w)/2,
		Y: avail.Y + (avail.H-h)/2,
		W: w, H: h,
	}
}

// drawSettingsPanel paints the menu.
func (a *App) drawSettingsPanel(scr uv.Screen) {
	if a.settingsPanel == nil {
		return
	}
	r := a.settingsPanelRect()
	a.settingsPanel.Draw(scr, uv.Rect(r.X, r.Y, r.W, r.H))
}

// layoutSpec turns the current tree into what the configuration file holds.
func (a *App) layoutSpec() map[string]any {
	type valued interface{ Values() map[string]any }

	var walk func(n *layout.Node) map[string]any
	walk = func(n *layout.Node) map[string]any {
		if n == nil {
			return nil
		}
		switch n.Kind {
		case layout.KindLeaf:
			out := map[string]any{"module": a.moduleName(n.PaneID)}
			if m, ok := a.modules[n.PaneID]; ok {
				if v, ok := m.(valued); ok {
					out["options"] = v.Values()
				}
			}
			return out
		case layout.KindStack:
			children := make([]any, 0, len(n.Children))
			for _, c := range n.Children {
				children = append(children, walk(c))
			}
			return map[string]any{"stack": children}
		default:
			axis := "horizontal"
			if n.Orientation == layout.Vertical {
				axis = "vertical"
			}
			children := make([]any, 0, len(n.Children))
			for _, c := range n.Children {
				children = append(children, walk(c))
			}
			return map[string]any{
				"split":    axis,
				"ratios":   append([]int(nil), n.Ratios...),
				"children": children,
			}
		}
	}
	return walk(a.root)
}

// moduleName is what a pane's module is called in the configuration.
func (a *App) moduleName(id layout.PaneID) string {
	if name, ok := a.moduleNames[id]; ok {
		return name
	}
	return "term"
}

// saveSettings writes what changed.
//
// Two paths, and the narrow one matters. Replacing the layout block loses the
// comments written inside it — a note next to a pane that has been rearranged
// has nowhere to go. So when the arrangement has not changed, only the focused
// pane's options are written, and every comment in the file survives. The wide
// path is taken only once the layout really did change, and then it says so.
func (a *App) saveSettings() error {
	if a.cfgPath == "" {
		a.setStatus("no configuration file to write to")
		return nil
	}

	if !a.layoutChanged {
		values := map[string]any{}
		if m, ok := a.modules[a.focus]; ok {
			if v, ok := m.(interface{ Values() map[string]any }); ok {
				values = v.Values()
			}
		}
		if len(values) == 0 {
			a.setStatus("nothing to save")
			return nil
		}
		if err := stg.WritePaneOptions(a.cfgPath, a.paneOrdinal(a.focus), values); err != nil {
			a.setStatus("%s", err.Error())
			return err
		}
		a.setStatus("settings saved to %s", a.cfgPath)
		return nil
	}

	if err := stg.ReplaceLayout(a.cfgPath, a.layoutSpec()); err != nil {
		a.setStatus("%s", err.Error())
		return err
	}
	a.layoutChanged = false
	a.setStatus("layout and settings saved — comments inside the layout block were replaced")
	return nil
}

// paneOrdinal is a pane's position in document order, which is how the
// configuration addresses it.
func (a *App) paneOrdinal(id layout.PaneID) int {
	for i, leaf := range layout.Leaves(a.root) {
		if leaf == id {
			return i
		}
	}
	return 0
}
