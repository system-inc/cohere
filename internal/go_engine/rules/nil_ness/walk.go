package nil_ness

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// walk is one function's visit: the context it reports through and the blocks it has visited.
type walk struct {
	// context is where findings go.
	context *rule.Context
	// seen marks each block, by index, that visit should ignore.
	seen []bool
}

// report hands a finding to the context. Like the original it ignores an instruction that corresponds to no syntax,
// whose position is not valid.
func (walk *walk) report(category string, position token.Pos, format string, arguments ...any) {
	if position.IsValid() {
		walk.context.Report(analysis.Diagnostic{
			Pos:      position,
			Category: category,
			Message:  fmt.Sprintf(format, arguments...),
		})
	}
}

// notNil reports a finding if value is provably nil.
func (walk *walk) notNil(stack []fact, instruction ir.Instruction, value ir.Value, description string) {
	if nilnessOf(stack, value) == isNil {
		walk.report("nilderef", originalPosition(walk.context.TypesInfo, instruction), "%s", description)
	}
}

// visit visits reachable blocks of the control flow graph in dominance order, maintaining a stack of dominating
// nilness facts.
//
// By traversing the dominator tree, we can pop facts off the stack as soon as we've visited a subtree. Had we
// traversed the control flow graph, we would need to retain the set of facts for each block.
func (walk *walk) visit(block *ir.BasicBlock, stack []fact) {
	if walk.seen[block.Index] {
		return
	}
	walk.seen[block.Index] = true

	// Report nil dereferences.
	for _, instruction := range block.Instrs {
		walk.checkDereference(stack, instruction)
	}

	// Look for panics with nil value.
	for _, instruction := range block.Instrs {
		switch instruction := instruction.(type) {
		case *ir.Panic:
			if nilnessOf(stack, instruction.X) == isNil {
				walk.report("nilpanic", originalPosition(walk.context.TypesInfo, instruction), "panic with nil value")
			}
		case *ir.SliceToArrayPointer:
			if nilnessOf(stack, instruction.X) == isNil && sliceToArrayPointerLength(instruction) > 0 {
				walk.report("conversionpanic", originalPosition(walk.context.TypesInfo, instruction),
					"nil slice being cast to an array of len > 0 will always panic")
			}
		}
	}

	// For nil comparison blocks, report an error if the condition is degenerate, and push a nilness fact on the
	// stack when visiting its true and false successor blocks.
	if comparison, trueSuccessor, falseSuccessor := equality(block); comparison != nil {
		xNilness := nilnessOf(stack, comparison.X)
		yNilness := nilnessOf(stack, comparison.Y)

		if yNilness != unknown && xNilness != unknown && (xNilness == isNil || yNilness == isNil) {
			// Degenerate condition: the nilness of both operands is known, and at least one of them is nil.
			walk.reportDegenerate(originalPosition(walk.context.TypesInfo, comparison), xNilness, comparison.Op,
				yNilness)

			// If trueSuccessor's or falseSuccessor's sole incoming edge is impossible, it is unreachable. Prune
			// traversal of it and all the blocks it dominates. (We could be more precise with full dataflow analysis
			// of control-flow joins.)
			skip := trueSuccessor
			if xNilness == yNilness {
				skip = falseSuccessor
			}
			for _, dominee := range block.Dominees() {
				if dominee == skip && len(dominee.Preds) == 1 {
					continue
				}
				walk.visit(dominee, stack)
			}
			return
		}

		// "if x == nil" or "if nil == y" condition; x, y are unknown.
		if xNilness == isNil || yNilness == isNil {
			var newFacts []fact
			if xNilness == isNil {
				// x is nil, y is unknown: the true successor learns y is nil.
				newFacts = expandFacts(fact{comparison.Y, isNil})
			} else {
				// y is nil, x is unknown: the true successor learns x is nil.
				newFacts = expandFacts(fact{comparison.X, isNil})
			}

			for _, dominee := range block.Dominees() {
				// Successor blocks learn a fact only at non-critical edges. (We could be more precise with full
				// dataflow analysis of control-flow joins.)
				dominated := stack
				if len(dominee.Preds) == 1 {
					if dominee == trueSuccessor {
						dominated = append(dominated, newFacts...)
					} else if dominee == falseSuccessor {
						dominated = append(dominated, negated(newFacts)...)
					}
				}
				walk.visit(dominee, dominated)
			}
			return
		}
	}

	// x/tools lowers a type switch into a chain of tests, `case nil` among them a comparison with nil, where
	// staticcheck dispatches every case from one block. visitTypeSwitch judges that comparison as the original does.
	if walk.visitTypeSwitch(block, stack) {
		return
	}

	// In code of the form:
	//
	// 	if pointer, ok := x.(*T); ok { ... } else { falseSuccessor }
	//
	// the falseSuccessor block learns that pointer == nil, since that's its zero value.
	if branch, isIf := block.Control().(*ir.If); isIf {
		// Handle "if ok" and "if !ok" variants.
		condition, falseSuccessor := branch.Cond, block.Succs[1]
		if negation, isNegation := condition.(*ir.UnOp); isNegation && negation.Op == token.NOT {
			condition, falseSuccessor = negation.X, block.Succs[0]
		}

		// Match pattern:
		//   t0 = typeassert (pointerlike)
		//   t1 = extract t0 #0  // pointer
		//   t2 = extract t0 #1  // ok
		//   if t2 goto trueSuccessor, falseSuccessor
		if okExtract, isExtract := condition.(*ir.Extract); isExtract && okExtract.Index == 1 {
			if assertion, isAssertion := okExtract.Tuple.(*ir.TypeAssert); isAssertion &&
				isNillable(assertion.AssertedType) {
				for _, referrer := range *assertion.Referrers() {
					if valueExtract, isValueExtract := referrer.(*ir.Extract); isValueExtract &&
						valueExtract.Index == 0 &&
						valueExtract.Tuple == okExtract.Tuple {
						for _, dominee := range block.Dominees() {
							if len(dominee.Preds) == 1 && dominee == falseSuccessor {
								walk.visit(dominee, append(stack, fact{valueExtract, isNil}))
							}
						}
					}
				}
			}
		}
	}

	for _, dominee := range block.Dominees() {
		walk.visit(dominee, stack)
	}
}

// checkDereference reports instruction if it dereferences a value the stack proves nil.
func (walk *walk) checkDereference(stack []fact, instruction ir.Instruction) {
	switch instruction := instruction.(type) {
	case ir.CallInstruction:
		// A nil receiver may be okay for type params.
		common := instruction.Common()
		// The original builds the message for every call before it asks whether the callee is nil; the port asks
		// first, so a call that is fine costs no message.
		if (!common.IsInvoke() || !isTypeParameter(common.Value.Type())) && nilnessOf(stack, common.Value) == isNil {
			walk.report("nilderef", originalPosition(walk.context.TypesInfo, instruction), "%s",
				"nil dereference in "+common.Description())
		}
	case *ir.FieldAddr:
		walk.notNil(stack, instruction, instruction.X, "nil dereference in field selection")
	case *ir.IndexAddr:
		switch coreType(instruction.X.Type()).(type) {
		case *types.Pointer: // *array
			if isLiteralElementInPlace(instruction) {
				return
			}
			walk.notNil(stack, instruction, instruction.X, "nil dereference in array index operation")
		case *types.Slice:
			// This is not necessarily a runtime error, because it is usually dominated by a bounds check.
			if isRangeIndex(instruction) {
				walk.notNil(stack, instruction, instruction.X, "range of nil slice")
			} else {
				walk.notNil(stack, instruction, instruction.X, "index of nil slice")
			}
		}
	case *ir.MapUpdate:
		walk.notNil(stack, instruction, instruction.Map, "nil dereference in map update")
	case *ir.Range:
		// (Not a runtime error, but a likely mistake.)
		walk.notNil(stack, instruction, instruction.X, "range over nil map")
	case *ir.Slice:
		// A nil check occurs in pointer[:] if and only if pointer is a pointer to an array.
		if _, isPointer := instruction.X.Type().Underlying().(*types.Pointer); isPointer {
			walk.notNil(stack, instruction, instruction.X, "nil dereference in slice operation")
		}
	case *ir.Store:
		walk.notNil(stack, instruction, instruction.Addr, "nil dereference in store")
	case *ir.TypeAssert:
		if !instruction.CommaOk {
			walk.notNil(stack, instruction, instruction.X, "nil dereference in type assertion")
		}
	// x/tools loads through a pointer with a unary `*` and receives with a unary `<-`, where staticcheck has an
	// instruction for each.
	case *ir.Load:
		walk.notNil(stack, instruction, instruction.X, "nil dereference in load")
	case *ir.Recv:
		// (Not a runtime error, but a likely mistake.)
		walk.notNil(stack, instruction, instruction.Chan, "receive from nil channel")
	case *ir.Send:
		// (Not a runtime error, but a likely mistake.)
		walk.notNil(stack, instruction, instruction.Chan, "send to nil channel")
	}
}

// reportDegenerate reports a comparison with nil whose outcome the nilness of both operands decides.
func (walk *walk) reportDegenerate(position token.Pos, xNilness nilState, operator token.Token, yNilness nilState) {
	adjective := "impossible"
	if (xNilness == yNilness) == (operator == token.EQL) {
		adjective = "tautological"
	}
	walk.report("cond", position, "%s condition: %s %s %s", adjective, xNilness, operator, yNilness)
}

// visitTypeSwitch visits the cases of the type switch block ends with, if it ends with one that has a `case nil`, and
// reports whether it did.
//
// x/tools tests a type switch's cases one after another in source order, the default last, and tests `case nil` as
// `x == nil` at the position of its nil: the true successor is the clause's body, and the false successor is the
// block that tests every later case, so the original judges that comparison as any other. staticcheck builds one
// TypeSwitch and one ConstantSwitch whose successors are a head for each case, in the same order, the default's or
// the switch's end last. So here the nil case's head stands for the true successor, and the blocks reached only
// through a later head stand for the blocks x/tools' false successor dominates.
func (walk *walk) visitTypeSwitch(block *ir.BasicBlock, stack []fact) bool {
	typeSwitch, constantSwitch, nilCase := typeSwitchNilCase(block)
	if typeSwitch == nil {
		return false
	}
	nilHead := block.Succs[nilCase]
	laterHeads := block.Succs[nilCase+1:]
	tagNilness := nilnessOf(stack, typeSwitch.Tag)

	if tagNilness != unknown {
		// Degenerate condition, as the original's: the nil case is taken always or never.
		walk.reportDegenerate(constantSwitch.Conds[nilCase].Pos(), tagNilness, token.EQL, isNil)
		skipped := []*ir.BasicBlock{nilHead}
		if tagNilness == isNil {
			skipped = laterHeads
		}
		reached := reachedAvoiding(block, skipped)
		for _, dominee := range block.Dominees() {
			if !reached[dominee.Index] {
				continue
			}
			walk.visit(dominee, stack)
		}
		return true
	}

	newFacts := expandFacts(fact{typeSwitch.Tag, isNil})
	reached := reachedAvoiding(block, laterHeads)
	for _, dominee := range block.Dominees() {
		dominated := stack
		if dominee == nilHead && len(dominee.Preds) == 1 {
			dominated = append(dominated, newFacts...)
		} else if !reached[dominee.Index] {
			dominated = append(dominated, negated(newFacts)...)
		}
		walk.visit(dominee, dominated)
	}
	return true
}

// isLiteralElementInPlace is whether address is where staticcheck writes an element of an array literal that does not
// give every element straight through the pointer the literal is assigned through, `*pointer = [3]int{1: x}`. Such an
// address comes from neither an index expression nor a range loop. x/tools builds the literal in a temporary and
// copies it whole through the pointer, a store both forms judge, so the original never sees an element's address.
func isLiteralElementInPlace(address *ir.IndexAddr) bool {
	switch address.Source().(type) {
	case *ast.IndexExpr, *ast.IndexListExpr, *ast.RangeStmt:
		return false
	}
	return true
}

// typeSwitchNilCase is the TypeSwitch and the ConstantSwitch that end block when they dispatch a type switch with a
// `case nil`, and the index of that case among the switch's conditions and block's successors, or nils when block
// ends otherwise.
func typeSwitchNilCase(block *ir.BasicBlock) (*ir.TypeSwitch, *ir.ConstantSwitch, int) {
	constantSwitch, isConstantSwitch := block.Control().(*ir.ConstantSwitch)
	if !isConstantSwitch {
		return nil, nil, 0
	}
	extract, isExtract := constantSwitch.Tag.(*ir.Extract)
	if !isExtract || extract.Index != 0 {
		return nil, nil, 0
	}
	typeSwitch, isTypeSwitch := extract.Tuple.(*ir.TypeSwitch)
	if !isTypeSwitch || len(block.Succs) != len(constantSwitch.Conds) {
		return nil, nil, 0
	}
	for index, condition := range typeSwitch.Conds {
		if basic, isBasic := condition.(*types.Basic); isBasic && basic.Kind() == types.UntypedNil &&
			index < len(constantSwitch.Conds) {
			return typeSwitch, constantSwitch, index
		}
	}
	return nil, nil, 0
}

// reachedAvoiding marks, by index, each block of block's function reachable from its entry, or from the block a
// recovered panic resumes at, without taking an edge from block to one of avoided. A block it leaves unmarked is
// reached only through those edges.
func reachedAvoiding(block *ir.BasicBlock, avoided []*ir.BasicBlock) []bool {
	reached := make([]bool, len(block.Parent().Blocks))
	pending := []*ir.BasicBlock{block.Parent().Blocks[0]}
	if block.Parent().Recover != nil {
		pending = append(pending, block.Parent().Recover)
	}
	for _, entry := range pending {
		reached[entry.Index] = true
	}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, successor := range current.Succs {
			if reached[successor.Index] || current == block && slices.Contains(avoided, successor) {
				continue
			}
			reached[successor.Index] = true
			pending = append(pending, successor)
		}
	}
	return reached
}
