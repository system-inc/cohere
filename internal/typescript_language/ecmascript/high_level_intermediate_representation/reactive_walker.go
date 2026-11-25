package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

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
