package intermediate_representation

import (
	"go/ast"
)

// expressionValue is what an expression lowered to: its value, or its address when isAddress is set.
type expressionValue struct {
	value     Value
	isAddress bool
}

// debugInformation reports whether function records the expression map: whether the package it is declared in
// was built with SetDebugMode. An instantiation follows its origin.
func (function *Function) debugInformation() bool {
	declared := function.declaredPackage()
	return declared != nil && declared.debug
}

// recordExpressions turns function's debug references into its expression map, the first reference to each
// expression winning, and strips them from its blocks, as go/ir's finishBody does. finishBody calls it after
// lifting has renamed the references with the rest.
func (function *Function) recordExpressions() {
	if !function.debugInformation() {
		return
	}
	function.expressionValues = map[ast.Expr]expressionValue{}
	dropping := liftingOptionsOf(function.Program).dropDebugReference
	for _, block := range function.Blocks {
		kept := block.Instructions[:0]
		for _, instruction := range block.Instructions {
			reference, isReference := instruction.(*debugReference)
			if !isReference {
				kept = append(kept, instruction)
				continue
			}
			if referrers := reference.X.Referrers(); referrers != nil {
				*referrers = liftingRemoveReferrer(*referrers, reference)
			}
			if dropping {
				dropping = false
				continue
			}
			if _, isRecorded := function.expressionValues[reference.Expression]; !isRecorded {
				function.expressionValues[reference.Expression] = expressionValue{value: reference.X,
					isAddress: reference.IsAddress}
			}
		}
		clear(block.Instructions[len(kept):])
		block.Instructions = kept
	}
}

// ValueForExpression returns the IR value expression lowered to in function, and whether that value is the
// address expression denotes rather than its value, as go/ir's ValueForExpr does.
//
// It returns nil when function's package was built without SetDebugMode, when expression isn't lexically within
// function, when it is a constant expression (whose value go/types.Info.Types holds), a reference to nil or a
// builtin, or when its value was optimized away.
//
// When expression is an addressable expression used in an lvalue context, the value is the address it denotes,
// and isAddress is true. The types of expression (or &expression, if isAddress) and of the value are equal, but
// for untyped booleans from comparisons.
func (function *Function) ValueForExpression(expression ast.Expr) (value Value, isAddress bool) {
	entry := function.expressionValues[ast.Unparen(expression)]
	return entry.value, entry.isAddress
}
