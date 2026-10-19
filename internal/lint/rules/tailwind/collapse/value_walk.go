// The exit half of upstream's `walk` over a value tree, for the passes that rewrite a node once its
// arguments are already rewritten.
//
// Ported from `src/walk.ts` at Tailwind 4.3.3, for the one shape the calc passes use: an `exit` hook
// that either leaves the node or returns `WalkAction.ReplaceSkip(node)` with exactly one node.
// walk.go's Walk is the enter-only traversal over the CSS tree; this is a different tree and the
// other hook.
package tailwind

// walkValueExit visits nodes depth-first, calling exit on each node after its arguments, with the
// function the node is an argument of, or nil at the top level. When exit returns true, its node
// takes the visited node's place.
//
// Upstream's exit-phase ReplaceSkip splices the replacement in and advances past it, so the
// replacement is never visited, and the parent's own exit later reads its arguments with the
// replacement already in place. A one-node replacement keeps every sibling's index, so assigning
// into the slice is that splice. Upstream descends into any node with a non-empty `nodes`, which in
// a value tree is a function with arguments.
func walkValueExit(nodes []ValueNode, parent *ValueNode, exit func(node *ValueNode, parent *ValueNode) (ValueNode, bool)) {
	for index := range nodes {
		if len(nodes[index].Nodes) > 0 {
			walkValueExit(nodes[index].Nodes, &nodes[index], exit)
		}
		if replacement, replaced := exit(&nodes[index], parent); replaced {
			nodes[index] = replacement
		}
	}
}
