// Package fixture is the syntax tree write scan's positive control (TestTheSyntaxTreeWriteScanNamesTheFixturesWrites):
// every line marked `want` writes the tree, and the scan must say what the mark says of it; every other write here is
// to the function's own copy, and the scan must say nothing of it.
package fixture

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// writesTheClass is no-dupe-class-members' old swap, inline: the class's member list replaced while a checker on
// another goroutine may read it (#72kn826).
func writesTheClass(node *ast.Node) {
	node.AsClassDeclaration().Members = nil // want: writes ClassDeclaration.Members
}

// swapsThroughAPointer is the old swap as it was written, through a helper handed the field's address. The address
// is what the scan names: the helper's write through a pointer to a pointer reads like a write to any variable.
func swapsThroughAPointer(node *ast.Node) {
	declaration := node.AsClassDeclaration()
	swap(&declaration.Members) // want: takes the address of ClassDeclaration.Members
}

func swap(members **ast.NodeList) {
	*members = &ast.NodeList{}
}

// picked hands back part of the tree's own list, so writing what it returns writes the tree, as tailwind's
// targetArguments once did.
func picked(list *ast.NodeList) []*ast.Node {
	return list.Nodes[:1]
}

// collected hands back a slice it built, so what it returns is its caller's own.
func collected(list *ast.NodeList) []*ast.Node {
	var nodes []*ast.Node
	nodes = append(nodes, list.Nodes...)
	return nodes
}

// collectInto appends through a pointer to a slice its caller holds.
func collectInto(node *ast.Node, into *[]*ast.Node) {
	*into = append(*into, node)
}

// nodeOf reads a node.
func nodeOf(node *ast.Node) ast.Kind {
	return node.Kind
}

// writesTheTree's other shapes: an element of a list, a field of a node, a symbol's table, a sort in place, and one
// of the ast package's own writers.
func writesTheTree(node *ast.Node, list *ast.NodeList, symbol *ast.Symbol, nodes []*ast.Node) {
	list.Nodes[0] = node                      // want: writes an element of NodeList.Nodes
	node.Flags |= 1                           // want: writes Node.Flags
	symbol.Members["name"] = symbol           // want: writes an entry of Symbol.Members
	nodes[0] = node                           // want: writes an element of a []*ast.Node
	slices.Reverse(list.Nodes)                // want: writes in place, by slices.Reverse, an element of NodeList.Nodes
	_ = append(list.Nodes[:0], list.Nodes...) // want: appends onto a slice of NodeList.Nodes
	ast.SetParentInChildren(node)             // want: calls ast.SetParentInChildren, which writes the *ast.Node
	node.Loc = list.Loc                       // want: writes Node.Loc
	copy(nodes, list.Nodes)                   // want: copies into a []*ast.Node
	for _, list.Nodes[0] = range nodes {      // want: writes an element of NodeList.Nodes
	}
	*list = ast.NodeList{} // want: writes the ast.NodeList a *ast.NodeList points to
	first := picked(list)
	first[0] = node // want: writes an element of a []*ast.Node
	var gathered []*ast.Node
	collectInto(node, &gathered)
	gathered[0] = node // want: writes an element of a []*ast.Node
}

// computesOnACopy writes only what it built, which the scan must leave alone: a fresh list and its fields, a fresh
// slice and its elements, a local value copy of a node's list, slices it sorted after cloning or a helper built, and
// a node's own address.
func computesOnACopy(node *ast.Node, list *ast.NodeList) int {
	kept := &ast.NodeList{Loc: list.Loc}
	kept.Nodes = append(kept.Nodes, list.Nodes...)
	kept.Loc = node.Loc
	members := make([]*ast.Node, 0, len(list.Nodes))
	members = append(members, list.Nodes...)
	if len(members) > 0 {
		members[0] = node
	}
	var copied ast.NodeList
	copied.Loc = list.Loc
	value := *list
	value.Loc = node.Loc
	sorted := slices.Clone(list.Nodes)
	slices.Reverse(sorted)
	seen := map[*ast.Node]bool{}
	seen[node] = true
	ordered := collected(list)
	slices.Reverse(ordered)
	ordered[0] = node
	kind := nodeOf(&node.AsExpressionStatement().Node)
	return int(kind) + len(ordered) + len(kept.Nodes) + len(members) + copied.Loc.Len() + value.Loc.Len() +
		len(sorted) + len(seen)
}
