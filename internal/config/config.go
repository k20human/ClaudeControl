// Package config reads the declarative layout. Stage 1 only reads; the
// settings module will write back through the YAML node tree later, which is
// why nothing here reformats the document.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"claudecontrol/internal/layout"
	"claudecontrol/internal/session"
)

// NodeSpec is one node of the configured layout.
type NodeSpec struct {
	Split    string         `yaml:"split,omitempty"`
	Ratios   []int          `yaml:"ratios,omitempty"`
	Children []*NodeSpec    `yaml:"children,omitempty"`
	Stack    []*NodeSpec    `yaml:"stack,omitempty"`
	Module   string         `yaml:"module,omitempty"`
	Options  map[string]any `yaml:"options,omitempty"`
}

// Config is the whole document.
type Config struct {
	Layout *NodeSpec  `yaml:"layout"`
	Alerts *AlertSpec `yaml:"alerts,omitempty"`

	// Claude is what every session this application opens is started with.
	Claude *ClaudeSpec `yaml:"claude,omitempty"`

	// Scrollback is how many lines each pane keeps once they have scrolled
	// off the top. A pointer for the same reason the alerts are: not written
	// and written zero are different answers, and zero — keep next to
	// nothing — is a choice somebody who never scrolls back is entitled to
	// make.
	Scrollback *int `yaml:"scrollback,omitempty"`
}

// ScrollbackOrDefault is how many lines a pane keeps, with the default filled
// in.
//
// It is the largest thing this application holds: a cell is 112 bytes, so a
// line of eighty columns costs about ten kilobytes once it is kept, and a
// session with the emulator's own default of ten thousand lines reaches 94
// MiB on its own.
func (c *Config) ScrollbackOrDefault() int {
	if c == nil || c.Scrollback == nil {
		return session.DefaultScrollback
	}
	return *c.Scrollback
}

// ClaudeSpec is the program a session runs and the arguments it runs with.
//
// It exists because a shell alias cannot reach here. Adding a flag to every
// `claude` is usually done with one — `alias claude='claude --effort max'` —
// and an alias is expanded by an interactive shell reading a command line.
// This application executes the program instead, so the alias never applied
// and the same command ran at two different efforts depending on where it was
// typed.
//
// What is written here reaches every session: those in this file, those alt+a
// opens, and those brought back from the sessions panel. A pane that gives
// arguments of its own has them appended after these, so a flag it repeats is
// the one that counts.
type ClaudeSpec struct {
	Bin  string   `yaml:"bin,omitempty"`
	Args []string `yaml:"args,omitempty"`
}

// ClaudeDefaults are that program and those arguments, or nothing at all.
func (c *Config) ClaudeDefaults() (bin string, args []string) {
	if c == nil || c.Claude == nil {
		return "", nil
	}
	return c.Claude.Bin, c.Claude.Args
}

// AlertSpec says how you are told that a conversation is waiting on you when
// you are not looking at the screen.
//
// Pointers rather than plain bools: the difference between "not written" and
// "written false" is the difference between a default and a decision, and a
// default that could not be overridden to off would be a bug.
type AlertSpec struct {
	Bell    *bool `yaml:"bell,omitempty"`
	Desktop *bool `yaml:"desktop,omitempty"`
}

// AlertsOrDefault is what the document asked for, with the defaults filled in.
//
// The bell is on: it is one byte, it is exactly what BEL is for, and a
// terminal that does not want it ignores it. The desktop notification is on
// too — being told when you are looking elsewhere is the whole point, and a
// machine with no notification program says so once rather than staying quiet.
func (c *Config) AlertsOrDefault() (bell, desktop bool) {
	bell, desktop = true, true
	if c == nil || c.Alerts == nil {
		return
	}
	if c.Alerts.Bell != nil {
		bell = *c.Alerts.Bell
	}
	if c.Alerts.Desktop != nil {
		desktop = *c.Alerts.Desktop
	}
	return
}

// PaneSpec is what a leaf needs in order to build its module.
type PaneSpec struct {
	Module  string
	Options map[string]any
}

// Path returns the configuration file location, honouring XDG_CONFIG_HOME.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.yaml"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "claudecontrol", "config.yaml")
}

// Default is the layout used when no configuration file exists: a single pane
// running Claude Code in the current directory.
func Default() *Config {
	return &Config{Layout: &NodeSpec{Module: "claude"}}
}

// Load reads the file. A missing file yields Default and no error — a first
// run must work. A malformed file is an error: replacing the user's layout
// with defaults would hide the mistake.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if c.Layout == nil {
		return nil, fmt.Errorf("config: %s has no %q key", path, "layout")
	}
	return &c, nil
}

// NodeFrom reads a layout the application wrote down itself.
//
// Through YAML rather than by hand: it is the same parser that reads what a
// person wrote, so a saved arrangement that would not have been accepted in
// the configuration file is not accepted here either.
func NodeFrom(v map[string]any) (*NodeSpec, error) {
	raw, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("config: saved layout: %w", err)
	}
	var spec NodeSpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("config: saved layout: %w", err)
	}
	return &spec, nil
}

// Build turns the specification into a layout tree and the pane specs that go
// with it. Leaf ids are assigned in document order, starting at 1.
func Build(c *Config) (*layout.Node, map[layout.PaneID]PaneSpec, error) {
	if c == nil || c.Layout == nil {
		return nil, nil, errors.New("config: empty configuration")
	}
	panes := make(map[layout.PaneID]PaneSpec)
	var next layout.PaneID
	root, err := build(c.Layout, panes, &next)
	if err != nil {
		return nil, nil, err
	}
	return root, panes, nil
}

func build(spec *NodeSpec, panes map[layout.PaneID]PaneSpec, next *layout.PaneID) (*layout.Node, error) {
	switch {
	case spec.Module != "":
		*next++
		panes[*next] = PaneSpec{Module: spec.Module, Options: spec.Options}
		return &layout.Node{Kind: layout.KindLeaf, PaneID: *next}, nil

	case len(spec.Stack) > 0:
		n := &layout.Node{Kind: layout.KindStack}
		for _, child := range spec.Stack {
			c, err := build(child, panes, next)
			if err != nil {
				return nil, err
			}
			n.Children = append(n.Children, c)
		}
		return n, nil

	case spec.Split != "":
		var o layout.Orientation
		switch spec.Split {
		case "horizontal":
			o = layout.Horizontal
		case "vertical":
			o = layout.Vertical
		default:
			return nil, fmt.Errorf("config: unknown split axis %q (want horizontal or vertical)", spec.Split)
		}
		if len(spec.Children) == 0 {
			return nil, fmt.Errorf("config: split %q has no children", spec.Split)
		}
		if len(spec.Ratios) > 0 && len(spec.Ratios) != len(spec.Children) {
			return nil, fmt.Errorf("config: split %q has %d ratios for %d children",
				spec.Split, len(spec.Ratios), len(spec.Children))
		}
		n := &layout.Node{Kind: layout.KindSplit, Orientation: o}
		for _, child := range spec.Children {
			c, err := build(child, panes, next)
			if err != nil {
				return nil, err
			}
			n.Children = append(n.Children, c)
		}
		if len(spec.Ratios) > 0 {
			n.Ratios = append(n.Ratios, spec.Ratios...)
		} else {
			for range n.Children {
				n.Ratios = append(n.Ratios, 1)
			}
		}
		return n, nil
	}
	return nil, errors.New("config: node has neither a module, a split nor a stack")
}
