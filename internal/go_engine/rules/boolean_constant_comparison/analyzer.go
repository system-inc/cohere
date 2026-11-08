// Package boolean_constant_comparison is staticcheck's S1002, a comparison of a boolean expression with a boolean
// constant, `x == true`, which is `x` written longer, ported as a check on types and syntax that reports through the
// kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1002/s1002.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the comparison. The original asks no pattern query: it
// walks the binary expressions through the inspector and judges each with code.IsInTest, code.IsBoolConst and
// code.BoolConst, which the port asks of the kit's ports of them, and reports through staticcheck_report.Report where
// the original reports through report.Report. The registry runs the port in the original's place, under its name, and
// the original stays only as the oracle: the parity tests beside this file run both over the same packages.
//
// The port parts from the original in one place, the fix of a negated comparison whose other side is itself an
// unparenthesized comparison of operands that are no booleans, `a < b == false`: the original writes `!a < b`, which
// negates a and does not compile, and the port writes `!(a < b)`. The finding and its message are the original's.
package boolean_constant_comparison

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every binary expression, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.BinaryExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		binary, isBinary := node.(*ast.BinaryExpr)
		if !isBinary {
			// Invariant: the dispatcher hands Node only the binary expressions it was asked for.
			panic("boolean_constant_comparison: the dispatcher handed over a node that is not a binary expression")
		}
		Check(context, binary)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1002",
	"S1002",
	"Flags a comparison of a boolean expression with a boolean constant: staticcheck's S1002, ported.",
	&Listener,
)

// Check judges one binary expression: an equality or inequality of a boolean constant, named by an identifier, and
// an expression whose every type is a boolean says no more than that expression or its negation, which the fix
// writes in the comparison's place.
func Check(context *rule.Context, binary *ast.BinaryExpr) {
	// A test may spell out what it compares, so a comparison in a test file is left alone.
	if staticcheck_code.IsInTest(context, binary) {
		return
	}
	if binary.Op != token.EQL && binary.Op != token.NEQ {
		return
	}
	// The constant is an identifier as written, never looked through parentheses, so `x == (true)` is no finding.
	isLeftConstant := staticcheck_code.IsBoolConst(context, binary.X)
	if !isLeftConstant && !staticcheck_code.IsBoolConst(context, binary.Y) {
		return
	}
	// A constant on the left is the one compared, even where the right is one too, and the right is the other side.
	var other ast.Expr
	var value bool
	if isLeftConstant {
		value = staticcheck_code.BoolConst(context, binary.X)
		other = binary.Y
	} else {
		value = staticcheck_code.BoolConst(context, binary.Y)
		other = binary.X
	}
	// A package that type-checks records a type for every expression, so one with none, which the original never
	// meets either, is skipped.
	otherType := context.TypesInfo.TypeOf(other)
	if otherType == nil || !isEveryTermBool(otherType) {
		return
	}
	isNegated := (binary.Op == token.EQL && !value) || (binary.Op == token.NEQ && value)
	rendered := staticcheck_report.Render(context, other)
	simplified := rendered
	if isNegated {
		simplified = "!" + rendered
	}
	// The leading negations fold into one or none, by how many there are: `!!y` is y, and `!!!y` is !y.
	withoutNegations := strings.TrimLeft(simplified, "!")
	if (len(simplified)-len(withoutNegations))%2 == 1 {
		withoutNegations = "!" + withoutNegations
	}
	simplified = withoutNegations
	// The original writes its message's text as the fix, and so does the port, but where the negation it puts in
	// front would bind to a leftmost operand that no negation takes, as in `a < b == false`, whose text `!a < b` does
	// not compile: there the port negates the comparison whole.
	fixText := simplified
	if isNegated && isUnnegatableComparison(context, other) {
		fixText = "!(" + rendered + ")"
	}
	staticcheck_report.Report(context, binary,
		"should omit comparison to bool constant, can be simplified to "+simplified,
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(edit.Fix("Simplify bool comparison", edit.ReplaceWithString(binary, fixText))))
}

// isEveryTermBool is whether every type of expressionType's type set has bool as its underlying type, as the
// original asks through typeutil.All: a type whose underlying type is bool, or a type parameter whose every term is
// one. An untyped boolean is none, and so is an interface or a type parameter whose type set names no terms, as one
// constrained by any, since typeutil.All holds of no empty list.
func isEveryTermBool(expressionType types.Type) bool {
	return typeutil.All(expressionType, func(term *types.Term) bool {
		basic, isBasic := term.Type().Underlying().(*types.Basic)
		return isBasic && basic.Kind() == types.Bool
	})
}

// isUnnegatableComparison is whether a negation written in front of expression's text would bind to an operand no
// negation takes: expression is a binary expression as written, not parenthesized, and its leftmost operand, where a
// prefixed `!` binds, is of no boolean type. A leftmost operand that is a boolean leaves the original's text right as
// it is: booleans compare only for equality or inequality, either of which negating one operand negates, and so
// the chain of comparisons that begins with it.
func isUnnegatableComparison(context *rule.Context, expression ast.Expr) bool {
	binary, isBinary := expression.(*ast.BinaryExpr)
	if !isBinary {
		return false
	}
	leftmost := binary.X
	for {
		inner, isInner := leftmost.(*ast.BinaryExpr)
		if !isInner {
			break
		}
		leftmost = inner.X
	}
	leftmostType := context.TypesInfo.TypeOf(leftmost)
	if leftmostType == nil {
		return false
	}
	return !typeutil.All(leftmostType, func(term *types.Term) bool {
		basic, isBasic := term.Type().Underlying().(*types.Basic)
		return isBasic && basic.Info()&types.IsBoolean != 0
	})
}
