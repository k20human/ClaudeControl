package app

import (
	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/module"
)

func (a *App) handleKey(e uv.KeyPressEvent) {
	if m, ok := a.modules[a.focus].(module.Inputter); ok {
		m.Key(e)
	}
}
