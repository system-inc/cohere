// Walking a ReactiveFunction: the traversal every remaining pass runs on.
//
// This is React's `visitors.ts` (`ReactiveScopes/visitors.ts` at `react_conformance.UpstreamSha`)
// reduced to the read-only half. Upstream ships a visitor and a transform in one file; only the
// visitor is here, because a transform has no consumer yet and an unexercised rewrite surface is
// the shape this package keeps finding declared and never constructed.
//
// # Why this is its own file rather than part of its first consumer
//
// Four passes import it: prune-non-escaping-scopes, promote-used-temporaries,
// merge-scopes-that-invalidate-together, and the small pruning passes. Writing it inside the first
// one buries a shared primitive in one consumer and guarantees the next three either reach into
// that file or write their own walk. The three-level input check that preceded this found the same
// thing from the other direction: `PruneNonEscapingScopes.ts` imports five names from `./visitors`
// and none of them existed here, which is the sixth consecutive stage whose blocker was one layer
// further out than the framing.
//
// # The shape, which is upstream's and is not the obvious one
//
// Every node has a `visitX` and a `traverseX`. `visitX` is what a subclass overrides; `traverseX`
// is the default recursion. An override that wants to see a node and keep descending calls
// `traverse`; one that wants to prune the subtree simply does not. Go has no subclassing, so the
// pair becomes a struct of optional function fields: a nil field means "recurse normally", and a
// non-nil one receives the node plus the traversal to continue with.
//
// That indirection is the whole reason this is not a plain recursive function. `PruneNonEscaping`
// needs to visit a scope, decide whether it escapes, and only then choose whether to walk its
// body -- a decision that cannot be expressed by a walk that always descends.
//
// # What it does not do
//
// It does not visit into nested functions. `ReactiveFunction` holds no nested bodies today: the
// tree is built per function and `Function.Functions` carries the nested ones, for the reason
// `BuildReactiveFunction` records -- an IdentifierId names one value in this function and a
// different value in a nested one, so a walk crossing that boundary would conflate them. Upstream's
// `visitReactiveFunctionValue` hook exists for the case where they are threaded together; it is
// omitted rather than declared-and-never-called.
package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// ReactiveVisitor is the set of hooks a pass can install on the tree walk.
//
// Every field is optional. A nil hook means the walk recurses with its default behaviour, which is
// what makes a pass that cares about one node type a three-line struct literal rather than a
// thirteen-case switch it has to keep exhaustive by hand.
//
// A non-nil hook receives the node and a `traverse` closure. Calling `traverse()` continues into the
// node's children; not calling it prunes the subtree. That is upstream's visit/traverse split, and
// it is the reason this is a hook table rather than a plain walk.
type ReactiveVisitor struct {
	// Place is called for every place the walk reaches, with the role it plays.
	//
	// `PlaceRole` is this package's own, and it carries strictly more than upstream's split into
	// `visitPlace` and `visitLValue`: a receiver is distinguishable from an ordinary read, which
	// two of the consuming passes need and which upstream recovers by re-matching the variant.
	Place func(order static_single_assignment.EvaluationOrder, place Place, role PlaceRole)

	// Instruction is called for each instruction statement. Call traverse to descend into its value.
	Instruction func(instruction *ReactiveInstruction, traverse func())

	// Value is called for each value, including the composite ones nested inside another value.
	Value func(order static_single_assignment.EvaluationOrder, value ReactiveValue, traverse func())

	// Terminal is called for each terminal statement. Call traverse to descend into its blocks.
	Terminal func(statement *ReactiveTerminalStatement, traverse func())

	// Scope is called for each scope block. Call traverse to descend into its instructions.
	//
	// The hook a pruning pass installs: deciding a scope does not escape and then not calling
	// traverse is how a subtree is skipped.
	Scope func(scope *ReactiveScopeBlock, traverse func())

	// Block is called for each block, including the function body and every nested block.
	Block func(block ReactiveBlock, traverse func())
}

// VisitReactiveFunction walks a function body with the given visitor.
//
// Upstream's `visitReactiveFunction`, which is one line: the walk is entirely the visitor's.
func VisitReactiveFunction(function *ReactiveFunction, visitor ReactiveVisitor) {
	if function == nil {
		return
	}
	walker := reactiveWalker{visitor: visitor}
	walker.visitBlock(function.Body)
}
