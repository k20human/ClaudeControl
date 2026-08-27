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
