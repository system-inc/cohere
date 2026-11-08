package bools

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// booleanOperator is `||` or `&&`, as the pass judges a chain of it.
type booleanOperator struct {
	// name is the operator's word in a message, "or" or "and".
	name string
	// token is the operator itself.
	token token.Token
	// suspectEquality is the comparison that, tested twice against two constants under this operator, makes the
	// chain always true (`!=` under `||`) or always false (`==` under `&&`).
	suspectEquality token.Token
}

// orOperator is `||`, suspect over `!=`.
var orOperator = booleanOperator{name: "or", token: token.LOR, suspectEquality: token.NEQ}

// andOperator is `&&`, suspect over `==`.
var andOperator = booleanOperator{name: "and", token: token.LAND, suspectEquality: token.EQL}

// checkRedundant reports an operand repeated in operands, `e || e` or `e && e`, at each repeat after the first in
// operands' order. The operands are free of side effects, so the repeat can only evaluate to what the first did.
func (operator booleanOperator) checkRedundant(pass *analysis.Pass, operands []ast.Expr) {
	// written holds each operand's source, as printed, seen so far.
	written := map[string]bool{}
	for _, operand := range operands {
		source := formatted(pass.Fset, operand)
		if written[source] {
			pass.Report(analysis.Diagnostic{
				Pos:     operand.Pos(),
				End:     operand.End(),
				Message: fmt.Sprintf("redundant %s: %s %s %s", operator.name, source, operator.token, source),
			})
			continue
		}
		written[source] = true
	}
}

// checkSuspect reports one value tested against two constants with the operator's suspect equality,
// `x != c1 || x != c2` or `x == c1 && x == c2`: always true or always false when the constants differ, and
// redundant, which checkRedundant reports, when they are the same. The operands are free of side effects.
func (operator booleanOperator) checkSuspect(pass *analysis.Pass, operands []ast.Expr) {
	// comparisons maps the source of each value tested to the source of the first comparison testing it.
	comparisons := map[string]string{}
	for _, operand := range operands {
		comparison, isBinary := operand.(*ast.BinaryExpr)
		if !isBinary || comparison.Op != operator.suspectEquality {
			continue
		}
		// Only a comparison with a constant is judged, to avoid false positives, and the value tested is its other
		// side. When both sides are constant, `runtime.GOOS == "windows"`, the left one is taken as the value, which
		// catches a repeated left side, the way most code is written.
		var tested ast.Expr
		switch {
		case pass.TypesInfo.Types[comparison.Y].Value != nil:
			tested = comparison.X
		case pass.TypesInfo.Types[comparison.X].Value != nil:
			tested = comparison.Y
		default:
			continue
		}
		testedSource := formatted(pass.Fset, tested)
		comparisonSource := formatted(pass.Fset, operand)
		first, testedBefore := comparisons[testedSource]
		if !testedBefore {
			comparisons[testedSource] = comparisonSource
			continue
		}
		// The same comparison twice is redundant, and checkRedundant reports it.
		if comparisonSource != first {
			pass.Report(analysis.Diagnostic{
				Pos:     operand.Pos(),
				End:     operand.End(),
				Message: fmt.Sprintf("suspect %s: %s %s %s", operator.name, comparisonSource, operator.token, first),
			})
		}
	}
}

// split is every operand of the chain of the operator that expression heads, looking through parentheses, from the
// last to the first, given `a || (b || c) || d`, {d, c, b, a}, and every operator of the chain it walked, expression
// among them, which run marks expanded so it does not judge an inner operator again.
func (operator booleanOperator) split(expression ast.Expr) ([]ast.Expr, []*ast.BinaryExpr) {
	operands := []ast.Expr{}
	operators := []*ast.BinaryExpr{}
	// remaining is the chain's left part, not yet split.
	remaining := expression
	for {
		unparenthesized := ast.Unparen(remaining)
		binary, isBinary := unparenthesized.(*ast.BinaryExpr)
		if !isBinary || binary.Op != operator.token {
			return append(operands, unparenthesized), operators
		}
		operators = append(operators, binary)
		rightOperands, rightOperators := operator.split(binary.Y)
		operands = append(operands, rightOperands...)
		operators = append(operators, rightOperators...)
		remaining = binary.X
	}
}
