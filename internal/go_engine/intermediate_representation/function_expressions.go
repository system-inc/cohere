package intermediate_representation

import (
	"cmp"
	"go/ast"
	"go/token"
	"slices"
)

// expressionValue is what an expression lowered to, one entry of a function's expression map: its value, or its
// address when isAddress is set. position is the expression's, which the map is sorted by.
type expressionValue struct {
	expression ast.Expr
	value      Value
	position   token.Pos
	isAddress  bool
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
//
// The map is a table of entries sorted by position, made at the size its references need, where a Go map of them
// took about three times the bytes as it grew (#npb86tz). The sort is stable, so of the references to one expression
// the first in block order comes first, and is the one ValueForExpression finds.
func (function *Function) recordExpressions() {
	if !function.debugInformation() {
		return
	}
	references := 0
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			if _, isReference := instruction.(*debugReference); isReference {
				references++
			}
		}
	}
	function.expressionValues = make([]expressionValue, 0, references)
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
			function.expressionValues = append(function.expressionValues, expressionValue{
				expression: reference.Expression,
				value:      reference.X,
				position:   reference.Expression.Pos(),
				isAddress:  reference.IsAddress,
			})
		}
		clear(block.Instructions[len(kept):])
		block.Instructions = kept
	}
	slices.SortStableFunc(function.expressionValues, func(first expressionValue, second expressionValue) int {
		return cmp.Compare(first.position, second.position)
	})
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
	unparenthesized := ast.Unparen(expression)
	if unparenthesized == nil {
		return nil, false
	}
	position := unparenthesized.Pos()
	// The first entry at the position, then each after it there, since expressions nested at one start share it: x
	// in x.f, f in f(x).
	first, _ := slices.BinarySearchFunc(function.expressionValues, position,
		func(entry expressionValue, target token.Pos) int {
			return cmp.Compare(entry.position, target)
		})
	for _, entry := range function.expressionValues[first:] {
		if entry.position != position {
			break
		}
		if entry.expression == unparenthesized {
			return entry.value, entry.isAddress
		}
	}
	return nil, false
}
