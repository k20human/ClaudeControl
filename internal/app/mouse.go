package app

import uv "github.com/charmbracelet/ultraviolet"

// dragState records an in-flight divider drag.
type dragState struct {
	div   int
	start int
}

func (a *App) handleMouse(ev uv.MouseEvent, m uv.Mouse) {}
