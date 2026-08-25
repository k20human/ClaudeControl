// Package layout holds the pane tree: how panes are nested, and where each one
// lands on screen. It has no dependency on a terminal, so it is fully testable
// in isolation.
package layout

import "fmt"

// PaneID identifies a leaf. Zero is never a valid id.
type PaneID uint64

// Orientation is the axis along which a split divides its area.
type Orientation int

const (
	// Horizontal places children side by side, dividing the width.
	Horizontal Orientation = iota
	// Vertical stacks children, dividing the height.
	Vertical
)

// NodeKind discriminates the three node types.
type NodeKind int

const (
	// KindLeaf is a single pane.
	KindLeaf NodeKind = iota
	// KindSplit divides its area between all its children.
	KindSplit
	// KindStack overlays its children; only Active is visible.
	KindStack
)

// Node is one node of the pane tree.
type Node struct {
	Kind NodeKind

	// Split only.
	Orientation Orientation
	Ratios      []int

	// Split and Stack.
	Children []*Node

	// Stack only: index into Children of the visible child.
	Active int

	// Leaf only.
	PaneID PaneID
}

// Find returns the leaf with the given id, its parent, and its index within
// that parent. parent is nil when the leaf is the root. All three results are
// zero when the id is absent.
func Find(root *Node, id PaneID) (node, parent *Node, index int) {
	if root == nil {
		return nil, nil, 0
	}
	if root.Kind == KindLeaf {
		if root.PaneID == id {
			return root, nil, 0
		}
		return nil, nil, 0
	}
	for i, c := range root.Children {
		if c.Kind == KindLeaf {
			if c.PaneID == id {
				return c, root, i
			}
			continue
		}
		if n, p, idx := Find(c, id); n != nil {
			if p == nil {
				// The match was c itself, which cannot happen for a non-leaf.
				return n, root, i
			}
			return n, p, idx
		}
	}
	return nil, nil, 0
}

// Leaves returns every leaf id in visual order.
func Leaves(root *Node) []PaneID {
	if root == nil {
		return nil
	}
	if root.Kind == KindLeaf {
		return []PaneID{root.PaneID}
	}
	var out []PaneID
	for _, c := range root.Children {
		out = append(out, Leaves(c)...)
	}
	return out
}

// Split inserts leaf next to the pane identified by target.
//
// When the target's parent already divides along o, leaf becomes a sibling and
// takes half of the target's ratio: the other siblings keep their size, which
// is what a user expects when splitting one pane among several. Otherwise the
// target is wrapped in a new split of two equal children.
func Split(root *Node, target PaneID, leaf *Node, o Orientation) (*Node, error) {
	if root == nil {
		return nil, fmt.Errorf("layout: split on an empty tree")
	}
	if leaf == nil || leaf.Kind != KindLeaf {
		return nil, fmt.Errorf("layout: split needs a leaf node")
	}
	node, parent, index := Find(root, target)
	if node == nil {
		return nil, fmt.Errorf("layout: pane %d not found", target)
	}

	if parent != nil && parent.Kind == KindSplit && parent.Orientation == o {
		half := parent.Ratios[index] / 2
		if half < 1 {
			half = 1
		}
		parent.Ratios[index] -= half
		if parent.Ratios[index] < 1 {
			parent.Ratios[index] = 1
		}
		parent.Children = insertAt(parent.Children, index+1, leaf)
		parent.Ratios = insertIntAt(parent.Ratios, index+1, half)
		return root, nil
	}

	wrapper := &Node{
		Kind:        KindSplit,
		Orientation: o,
		Ratios:      []int{50, 50},
		Children:    []*Node{node, leaf},
	}
	if parent == nil {
		return wrapper, nil
	}
	parent.Children[index] = wrapper
	return root, nil
}

// Remove deletes a leaf. A split left with a single child is replaced by that
// child, so the tree never keeps a node that divides nothing. Removing the last
// leaf yields a nil root.
func Remove(root *Node, id PaneID) (*Node, error) {
	if root == nil {
		return nil, fmt.Errorf("layout: remove from an empty tree")
	}
	if root.Kind == KindLeaf {
		if root.PaneID != id {
			return nil, fmt.Errorf("layout: pane %d not found", id)
		}
		return nil, nil
	}
	node, parent, index := Find(root, id)
	if node == nil {
		return nil, fmt.Errorf("layout: pane %d not found", id)
	}

	parent.Children = removeAt(parent.Children, index)
	if parent.Kind == KindSplit {
		parent.Ratios = removeIntAt(parent.Ratios, index)
	}
	if parent.Kind == KindStack && parent.Active >= len(parent.Children) {
		parent.Active = len(parent.Children) - 1
	}
	if len(parent.Children) == 1 {
		collapse(parent)
	}
	return root, nil
}

// collapse replaces target with its single remaining child, in place. Parents
// hold *Node, so overwriting the struct keeps every existing pointer valid.
func collapse(target *Node) {
	only := target.Children[0]
	*target = *only
}

func insertAt(s []*Node, i int, v *Node) []*Node {
	s = append(s, nil)
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

func insertIntAt(s []int, i, v int) []int {
	s = append(s, 0)
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

func removeAt(s []*Node, i int) []*Node { return append(s[:i], s[i+1:]...) }

func removeIntAt(s []int, i int) []int { return append(s[:i], s[i+1:]...) }
