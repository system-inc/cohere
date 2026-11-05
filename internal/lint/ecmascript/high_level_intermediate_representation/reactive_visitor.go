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

// reactiveWalker carries the visitor so the recursion does not thread it through every call.
//
// Its methods are named `visitX` rather than the bare `x` upstream uses, and that is a hard
// constraint rather than a preference: `hir_test.go` discovers the closed terminal and
// instruction-value sets by scanning the package source for methods named `terminal` and
// `instructionValue`, so a method named `terminal` on any type registers as a terminal variant and
// fails five exhaustiveness tests at once. That guard caught this exact collision when this file
// first landed.
type reactiveWalker struct {
	visitor ReactiveVisitor
}

func (walker *reactiveWalker) visitBlock(block ReactiveBlock) {
	if walker.visitor.Block != nil {
		walker.visitor.Block(block, func() { walker.traverseBlock(block) })
		return
	}
	walker.traverseBlock(block)
}

func (walker *reactiveWalker) traverseBlock(block ReactiveBlock) {
	for _, statement := range block {
		switch shape := statement.(type) {
		case *ReactiveInstructionStatement:
			walker.visitInstruction(shape.Instruction)
		case *ReactiveScopeBlock:
			walker.visitScope(shape)
		case *ReactiveTerminalStatement:
			walker.visitTerminal(shape)
		}
	}
}

func (walker *reactiveWalker) visitScope(scope *ReactiveScopeBlock) {
	if walker.visitor.Scope != nil {
		walker.visitor.Scope(scope, func() { walker.visitBlock(scope.Instructions) })
		return
	}
	walker.visitBlock(scope.Instructions)
}

func (walker *reactiveWalker) visitInstruction(instruction *ReactiveInstruction) {
	if instruction == nil {
		return
	}
	if walker.visitor.Instruction != nil {
		walker.visitor.Instruction(instruction, func() { walker.traverseInstruction(instruction) })
		return
	}
	walker.traverseInstruction(instruction)
}

func (walker *reactiveWalker) traverseInstruction(instruction *ReactiveInstruction) {
	// The lvalue first, matching upstream's order: `traverseInstruction` visits lvalues before the
	// value. A pass building a definition table reads it before the uses that may shadow it.
	if instruction.LValue != nil {
		walker.visitPlace(instruction.Order, *instruction.LValue, PlaceRoleDefine)
	}
	walker.visitValue(instruction.Order, instruction.Value)
}

func (walker *reactiveWalker) visitValue(order static_single_assignment.EvaluationOrder, value ReactiveValue) {
	if value == nil {
		return
	}
	if walker.visitor.Value != nil {
		walker.visitor.Value(order, value, func() { walker.traverseValue(order, value) })
		return
	}
	walker.traverseValue(order, value)
}

func (walker *reactiveWalker) traverseValue(order static_single_assignment.EvaluationOrder, value ReactiveValue) {
	switch shape := value.(type) {
	case *ReactiveInstructionValue:
		// The leaf. A plain instruction value's places are read through this package's own
		// `EachPlace`, which is why this walk carries a role and upstream's does not.
		if shape.Value != nil {
			EachPlace(shape.Value, func(place Place, role PlaceRole) {
				walker.visitPlace(order, place, role)
			})
		}
	case *ReactiveLogicalValue:
		walker.visitValue(order, shape.Left)
		walker.visitValue(order, shape.Right)
	case *ReactiveTernaryValue:
		walker.visitValue(order, shape.Test)
		walker.visitValue(order, shape.Consequent)
		walker.visitValue(order, shape.Alternate)
	case *ReactiveSequenceValue:
		// The instructions run for effect and the value is the result. Upstream visits the
		// instructions as instructions rather than as values, which matters: a pass counting
		// instructions sees the ones nested inside a sequence, and a pass that only walked values
		// would miss them. That is the same omission that cost this package 27 functions once.
		for _, nested := range shape.Instructions {
			walker.visitInstruction(nested)
		}
		walker.visitValue(shape.Order, shape.Value)
	case *ReactiveOptionalValue:
		walker.visitValue(shape.Order, shape.Value)
	}
}

func (walker *reactiveWalker) visitPlace(order static_single_assignment.EvaluationOrder, place Place, role PlaceRole) {
	if walker.visitor.Place != nil {
		walker.visitor.Place(order, place, role)
	}
}

func (walker *reactiveWalker) visitTerminal(statement *ReactiveTerminalStatement) {
	if statement == nil {
		return
	}
	if walker.visitor.Terminal != nil {
		walker.visitor.Terminal(statement, func() { walker.traverseTerminal(statement) })
		return
	}
	walker.traverseTerminal(statement)
}

// traverseTerminal descends into a terminal's operands and blocks, in upstream's order.
//
// The order is load-bearing rather than cosmetic. A `for` visits init, test, loop, then update,
// which is evaluation order and not field order; a `do-while` visits its body before its test for
// the same reason. A pass building a def-use table over this walk reads a wrong answer if either is
// reordered, and nothing about the resulting tree would look wrong.
func (walker *reactiveWalker) traverseTerminal(statement *ReactiveTerminalStatement) {
	switch shape := statement.Terminal.(type) {
	case *ReactiveBreak, *ReactiveContinue:
		// No operands and no blocks.

	case *ReactiveReturn:
		walker.visitPlace(shape.Order, shape.Value, PlaceRoleUse)

	case *ReactiveThrow:
		walker.visitPlace(shape.Order, shape.Value, PlaceRoleUse)

	case *ReactiveIf:
		walker.visitPlace(shape.Order, shape.Test, PlaceRoleUse)
		walker.visitBlock(shape.Consequent)
		if shape.Alternate != nil {
			walker.visitBlock(*shape.Alternate)
		}

	case *ReactiveSwitch:
		walker.visitPlace(shape.Order, shape.Test, PlaceRoleUse)
		for _, arm := range shape.Cases {
			// A nil Test is the default arm and a nil Block is a fallthrough arm sharing the next
			// one's statements. Both nils are meaningful and distinct; see ReactiveSwitchCase.
			if arm.Test != nil {
				walker.visitPlace(shape.Order, *arm.Test, PlaceRoleUse)
			}
			if arm.Block != nil {
				walker.visitBlock(*arm.Block)
			}
		}

	case *ReactiveFor:
		walker.visitValue(shape.Order, shape.Init)
		walker.visitValue(shape.Order, shape.Test)
		walker.visitBlock(shape.Loop)
		if shape.Update != nil {
			walker.visitValue(shape.Order, shape.Update)
		}

	case *ReactiveForOf:
		walker.visitValue(shape.Order, shape.Init)
		walker.visitValue(shape.Order, shape.Test)
		walker.visitBlock(shape.Loop)

	case *ReactiveForIn:
		walker.visitValue(shape.Order, shape.Init)
		walker.visitBlock(shape.Loop)

	case *ReactiveWhile:
		walker.visitValue(shape.Order, shape.Test)
		walker.visitBlock(shape.Loop)

	case *ReactiveDoWhile:
		// Body before test: a do-while runs its body first, and this walk is in evaluation order.
		walker.visitBlock(shape.Loop)
		walker.visitValue(shape.Order, shape.Test)

	case *ReactiveLabelTerminal:
		walker.visitBlock(shape.Block)

	case *ReactiveTry:
		walker.visitBlock(shape.Block)
		if shape.HandlerBinding != nil {
			walker.visitPlace(shape.Order, *shape.HandlerBinding, PlaceRoleDefine)
		}
		walker.visitBlock(shape.Handler)
	}
}
