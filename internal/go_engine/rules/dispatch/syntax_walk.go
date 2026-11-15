package dispatch

import (
	"go/ast"
	"reflect"
	"slices"

	"golang.org/x/tools/go/ast/inspector"
)

// syntaxWalk is the walk of one package's files, which keeps the ancestors of the node it visits as it goes.
type syntaxWalk struct {
	started *startedRuns
	// spine is the cursors from the file down to the node last visited, that node included, and ancestors is their
	// nodes, the slice a listener is handed with the last left out. Both are reused across the whole walk.
	spine     []inspector.Cursor
	ancestors []ast.Node
}

// visit calls every started listener that lists cursor's node's kind, with the node, its ancestors and its cursor,
// and descends into it.
func (walk *syntaxWalk) visit(cursor inspector.Cursor) bool {
	walk.descendTo(cursor)
	node := cursor.Node()
	// Capped at the ancestors, so a listener that appends to its stack makes a copy rather than overwriting the node
	// in the slice the walk goes on handing its descendants.
	ancestors := walk.ancestors[: len(walk.ancestors)-1 : len(walk.ancestors)-1]
	runs := walk.started.byKind[reflect.TypeOf(node)]
	for called := 0; called < len(runs); {
		called = walk.callNodes(runs, called, cursor, node, ancestors)
	}
	return true
}

// callNodes calls the Node of each of runs from the one at from on, in order, on node with its ancestors, each
// listener's context handing it cursor for the length of its call, and returns how far it got: past the last run, or
// past the one whose listener panicked, which it records as that run's crash, so the caller goes on with the rest.
//
// One recover serves every call on the node, where a call of its own each cost a deferred call and a recover for every
// listener a node reaches, about ten per node visited (#fex6s0v). It cannot change a finding: each listener is called
// in the same order with what it was handed before, and one that panics is the same crash, spelled from the same
// frames, and is called no more on the package. A Node that panics leaves its context's cursor held, which is
// harmless: the run has crashed, and nothing calls it again.
func (walk *syntaxWalk) callNodes(
	runs []*listenerRun,
	from int,
	cursor inspector.Cursor,
	node ast.Node,
	ancestors []ast.Node,
) (next int) {
	calling := from
	defer func() {
		if recovered := recover(); recovered != nil {
			runs[calling].crashed(recovered)
			next = calling + 1
		}
	}()
	for ; calling < len(runs); calling++ {
		run := runs[calling]
		if run.crash != nil {
			continue
		}
		run.context.EnterNode(cursor)
		if run.timing != nil {
			run.measured(func() { run.registered.listener.Node(run.context, node, ancestors) })
		} else {
			run.registered.listener.Node(run.context, node, ancestors)
		}
		run.context.LeaveNode()
	}
	return calling
}

// descendTo makes the spine end at cursor. The walk is in preorder, so of the spine of the node visited last, the
// part that encloses cursor is a prefix, and cursor's ancestors past it are the nodes between, which the inspector
// skipped as kinds no listener lists. It pops the rest and climbs from cursor to that prefix, so each node is pushed
// once as the walk first reaches it and popped once after, as the inspector's own stack keeps them, rather than
// every node's ancestors read again for every node visited.
func (walk *syntaxWalk) descendTo(cursor inspector.Cursor) {
	kept := len(walk.spine)
	for kept > 0 && !walk.spine[kept-1].Contains(cursor) {
		kept--
	}
	walk.spine = walk.spine[:kept]
	walk.ancestors = walk.ancestors[:kept]
	// Climbing ends at the deepest node kept, or for a file, whose parent is the root, at the root, the one cursor
	// with no node.
	for climbed := cursor; climbed.Node() != nil; climbed = climbed.Parent() {
		if kept > 0 && climbed == walk.spine[kept-1] {
			break
		}
		walk.spine = append(walk.spine, climbed)
		walk.ancestors = append(walk.ancestors, climbed.Node())
	}
	slices.Reverse(walk.spine[kept:])
	slices.Reverse(walk.ancestors[kept:])
}
