package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrUnreadable marks a configuration file that could not be parsed.
var ErrUnreadable = errors.New("settings: the configuration file cannot be parsed")

// indent matches what a hand-written file uses.
const indent = 2

// WritePaneOptions changes keys in one pane's options, in place.
//
// It goes through the YAML syntax tree rather than re-encoding a struct,
// because a struct round-trip drops every comment. Moving a slider must not
// cost somebody the notes they wrote next to it.
func WritePaneOptions(path string, ordinal int, values map[string]any) error {
	doc, err := load(path)
	if err != nil {
		return err
	}
	root := docRoot(doc)
	if root == nil {
		return fmt.Errorf("settings: %s has no mapping at its root", path)
	}
	layout := mapValue(root, "layout")
	if layout == nil {
		return fmt.Errorf("settings: %s has no %q key", path, "layout")
	}

	n := ordinal
	leaf := findLeaf(layout, &n)
	if leaf == nil {
		return fmt.Errorf("settings: %s has no pane number %d", path, ordinal)
	}
	options := mapValue(leaf, "options")
	if options == nil {
		options = &yaml.Node{Kind: yaml.MappingNode}
		leaf.Content = append(leaf.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "options"}, options)
	}
	for k, v := range values {
		setKey(options, k, v)
	}
	return save(path, doc)
}

// ReplaceLayout swaps the whole layout for a new one.
//
// Comments inside the layout block do not survive, and cannot: a note attached
// to a pane that no longer exists has nowhere to go. Everything outside the
// block — including its own leading comment — is kept.
func ReplaceLayout(path string, layout any) error {
	doc, err := load(path)
	if err != nil {
		return err
	}
	root := docRoot(doc)
	if root == nil {
		root = &yaml.Node{Kind: yaml.MappingNode}
		doc.Content = []*yaml.Node{root}
	}
	var encoded yaml.Node
	if err := encoded.Encode(layout); err != nil {
		return fmt.Errorf("settings: encode layout: %w", err)
	}
	setNode(root, "layout", &encoded)
	return save(path, doc)
}

// load reads a document, or returns an empty one for a file that is not there.
func load(path string) (*yaml.Node, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode}},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("settings: read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrUnreadable, path, err)
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode}},
		}
	}
	return &doc, nil
}

// save writes a document, atomically.
func save(path string, doc *yaml.Node) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("settings: directory: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("settings: write %s: %w", tmp, err)
	}
	enc := yaml.NewEncoder(f)
	enc.SetIndent(indent)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("settings: encode: %w", err)
	}
	_ = enc.Close()
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("settings: close: %w", err)
	}
	// Rename rather than write in place: a crash halfway through must not
	// leave a half-written configuration where a good one used to be.
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("settings: replace %s: %w", path, err)
	}
	return nil
}

// docRoot returns the mapping at the root of a document.
func docRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		if doc.Content[0].Kind == yaml.MappingNode {
			return doc.Content[0]
		}
	}
	return nil
}

// mapValue returns the value node for a key of a mapping, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setNode puts a value node under a key, replacing what was there.
func setNode(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			// The key node is left alone so its comment stays attached.
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
}

// setKey puts a scalar under a key, keeping the comments already on it.
func setKey(m *yaml.Node, key string, v any) {
	var encoded yaml.Node
	if err := encoded.Encode(v); err != nil {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old := m.Content[i+1]
			// Only the value changes. Carrying the comments across is what
			// keeps "# validated by eye" next to the number it describes.
			encoded.HeadComment = old.HeadComment
			encoded.LineComment = old.LineComment
			encoded.FootComment = old.FootComment
			m.Content[i+1] = &encoded
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key}, &encoded)
}

// findLeaf walks the layout in document order and returns the nth pane.
//
// Document order is what config.Build numbers panes by, so an ordinal held by
// a caller addresses the same pane here as it does there.
func findLeaf(n *yaml.Node, remaining *int) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.MappingNode && mapValue(n, "module") != nil {
		if *remaining == 0 {
			return n
		}
		*remaining--
		return nil
	}
	for _, key := range []string{"children", "stack"} {
		list := mapValue(n, key)
		if list == nil || list.Kind != yaml.SequenceNode {
			continue
		}
		for _, child := range list.Content {
			if found := findLeaf(child, remaining); found != nil {
				return found
			}
		}
	}
	return nil
}
