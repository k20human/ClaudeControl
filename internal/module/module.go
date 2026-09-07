// Package module defines what every pane content must implement, whatever it
// draws: a hosted process, a hologram, a service list.
package module

import (
	uv "github.com/charmbracelet/ultraviolet"

	"claudecontrol/internal/bus"
	"claudecontrol/internal/layout"
	"claudecontrol/internal/pool"
	"claudecontrol/internal/settings"
)

// Context carries what a module needs from the application.
type Context struct {
	PaneID layout.PaneID

	// Pool owns the sessions. A module that starts one hands it over rather
	// than keeping it: the session outlives the pane.
	Pool *pool.Pool

	// Bus is where facts are published and subscribed to.
	Bus *bus.Bus

	// HookSocket and Binary are what a session needs in order to report its
	// state: the socket to send to, and the executable that does the sending.
	HookSocket string
	Binary     string

	// Wake asks the application to redraw. It never blocks and may coalesce.
	Wake func()

	// Status puts a sentence in the application's bar. It is how a module
	// answers something it was asked to do but could not — closing the last
	// tab of a pane, say. Nil when nothing is listening, so callers check.
	Status func(string)
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

// Titler is implemented by modules that have something better to be called
// than the name they were registered under — or nothing at all.
type Titler interface {
	// Title is what belongs in the pane's title row. Reporting false asks for
	// no title row: the module keeps the whole pane, which is what a purely
	// visual pane wants.
	Title() (string, bool)
}

// Held is everything about a hosted module except the module itself: what it
// is called, what it was built from, and what it was built with.
//
// It travels when something moves — a tab carried to another pane, or promoted
// into a pane of its own — so that wherever it lands can be written down as
// what it is. Only some modules can describe themselves; a term cannot, and
// one recorded by name alone comes back as a bare shell.
type Held struct {
	Title   string
	Name    string
	Options map[string]any

	// Given says the title was typed by hand rather than derived. It travels
	// with the tab: a name you chose is not undone by moving what wears it.
	Given bool
}

// Sessioner is implemented by modules that hold conversations worth bringing
// back. A module holding several — a pane of tabs — reports them all, in the
// order they appear.
//
// A shell implements nothing here on purpose: a fresh shell is not something
// to resume.
type Sessioner interface {
	Sessions() []string
}

// Provider is implemented by modules that let you change something. The menu
// is built from what they publish, so a module gains an editable setting by
// describing it rather than by drawing a widget.
type Provider interface {
	Settings() []settings.Setting
}

// Inputter is implemented by modules that accept input. Coordinates in mouse
// events are already pane-local when the application calls Mouse.
type Inputter interface {
	Key(k uv.KeyEvent)
	Mouse(m uv.MouseEvent)
	Paste(text string)
}
