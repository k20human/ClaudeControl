// Package module defines what every pane content must implement, whatever it
// draws: a hosted process, a hologram, a service list.
package module

import (
	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/session"
)

// Context carries what a module needs from the application.
type Context struct {
	PaneID   layout.PaneID
	Sessions *session.Registry
	// Wake asks the application to redraw. It never blocks and may coalesce.
	Wake func()
}

// Module is one pane's content.
type Module interface {
	// Init is called once, before the first Draw.
	Init(ctx Context) error
	// Resize tells the module its new size in cells.
	Resize(w, h int) error
	// Draw paints the module into area of scr. It must not write outside area.
	Draw(scr uv.Screen, area uv.Rectangle)
	// Close releases everything the module owns.
	Close() error
}

// Cursorer is implemented by modules that own a cursor. The application places
// the real terminal cursor from the focused module only.
type Cursorer interface {
	Cursor() (x, y int, visible bool)
}

// Inputter is implemented by modules that accept input. Coordinates in mouse
// events are already pane-local when the application calls Mouse.
type Inputter interface {
	Key(k uv.KeyEvent)
	Mouse(m uv.MouseEvent)
	Paste(text string)
}
