// Package redundant_nil_check_on_length is staticcheck's S1009, a nil check beside a length check on a slice, a map
// or a channel, `x != nil && len(x) != 0`, where len already answers for nil since a nil one's length is zero,
// ported as a check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1009/s1009.go, whose logic it keeps and whose
// message it reports word for word, at the same range, the whole outer expression. The original matches one query,
// `(BinaryExpr (BinaryExpr x lhsOp@(Or "==" "!=") nilly) outerOp@(Or "&&" "||") (BinaryExpr (CallExpr (Builtin
// "len") [x]) rhsOp k))`, through staticcheck's pattern language with code.Matches, which consults no type index for
// it, since the query names no symbol; the port asks the same of each binary expression directly, reading
// pattern/match.go for what each construct of the query matches, and reports through staticcheck_report.Report
// where the original reports through report.Report. It suggests no fix, as the original does not, and keeps nothing
// from one expression to the next.
//
// It parts from the original in one place, where the original is wrong: the length the comparison is against. The
// original judges it by its spelling, zero or not zero, and takes every value it calls not zero to be positive, so it
// reports `x == nil || len(x) <= n` for a constant n of -1, and `x != nil && len(x) == z` for an untyped constant z
// of 0.0 or a rune literal '\x00', which it calls not zero; each is a nil check that changes the answer. And it
// leaves alone `x == nil || len(x) == 0.0`, whose nil check is redundant. The port reads the constant's value and
// asks what the original means to ask, whether a nil x, of length zero, makes the length check answer what the nil
// check would, so it drops the first kind and finds the second. The registry still runs the original, which stays
// the oracle: the parity tests beside this file run both over the same packages, and the differences fixture records
// each finding the port drops or finds.
package redundant_nil_check_on_length

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, since this check filters them. The original
// declares no flags, so neither does the port.
var Analyzer = &analysis.Analyzer{
	Name:     "S1009",
	Doc:      "Flags a nil check beside a length check that len already covers: staticcheck's S1009, ported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every binary expression of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("redundant_nil_check_on_length: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.BinaryExpr)(nil)}, func(node ast.Node) {
		binary, isBinary := node.(*ast.BinaryExpr)
		if !isBinary {
			// Invariant: Preorder hands this function only the binary expressions it was asked for.
			panic("redundant_nil_check_on_length: the inspector handed over a node that is not a binary expression")
		}
		Check(context, binary)
	})
	return nil, nil
}

// Check judges one binary expression: `x == nil || len(x) op k` or `x != nil && len(x) op k`, for an x free of side
// effects whose every type is a slice, a map or a channel, and a constant k for which a nil x, whose length is zero,
// makes the length check answer what the nil check would, so the nil check adds nothing.
func Check(context *rule.Context, outer *ast.BinaryExpr) {
	// `outerOp@(Or "&&" "||")`: the operator of the outer expression, which says what the nil check must be.
	var isEqualNil bool
	switch outer.Op {
	case token.LOR:
		isEqualNil = true
	case token.LAND:
		isEqualNil = false
	default:
		return
	}
	// The pattern looks through parentheses on every operand, so `(x != nil) && (len(x) > 0)` is the same shape.
	nilCheck, isNilCheckBinary := ast.Unparen(outer.X).(*ast.BinaryExpr)
	if !isNilCheckBinary {
		return
	}
	lengthCheck, isLengthCheckBinary := ast.Unparen(outer.Y).(*ast.BinaryExpr)
	if !isLengthCheckBinary {
		return
	}
	// `lhsOp@(Or "==" "!=")`, and then the original keeps only the one that fits the outer operator: == beside ||,
	// != beside &&, each the check whose short circuit len would have to repeat.
	if (isEqualNil && nilCheck.Op != token.EQL) || (!isEqualNil && nilCheck.Op != token.NEQ) {
		return
	}
	// `(CallExpr (Builtin "len") [x])`: a call of the universe's len, its name parenthesized or not, with one
	// argument; the pattern ignores a spread, which len never takes in code that type-checks.
	call, isCall := ast.Unparen(lengthCheck.X).(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 || !staticcheck_code.IsBuiltin(context, call.Fun, "len") {
		return
	}
	// x is bound first on the nil check's left, a node, so the length's argument recalls it by the pattern's syntax
	// comparison, identifiers by name, and a binding holds the node with its parentheses removed.
	checked := ast.Unparen(nilCheck.X)
	if !staticcheck_code.SameSyntax(checked, call.Args[0]) {
		return
	}
	// A call or a receive could answer differently the second time, and then the nil check is no repeat. The
	// original passes no purity result, so every call counts.
	if staticcheck_code.MayHaveSideEffects(context, checked, nil) {
		return
	}
	// nilly is bound to anything, its parentheses removed, and must be the predeclared nil.
	if !staticcheck_code.IsNil(context, ast.Unparen(nilCheck.Y)) {
		return
	}
	if !isRedundant(context, isEqualNil, lengthCheck.Op, ast.Unparen(lengthCheck.Y)) {
		return
	}
	nilKinds, isNillable := nilKindsOf(context.TypesInfo.TypeOf(checked))
	if !isNillable {
		return
	}
	staticcheck_report.Report(context, outer,
		fmt.Sprintf("should omit nil check; len() for %s is defined as zero", nilKinds),
		staticcheck_report.FilterGenerated())
}

// isRedundant is whether a nil x, of length zero, makes `len(x) operator bound` answer what the nil check on its
// left would: true beside `x == nil ||`, false beside `x != nil &&`, so dropping the nil check changes nothing. It
// asks only of the comparisons the original takes, == <= < beside || and == >= != > beside &&, and only of a bound
// the original reads, a literal or the name of a constant.
//
// Here the port parts from the original, which reads a literal as zero only when it is an integer literal of value
// zero and a constant's value only when its kind is integer, and takes any other value to be positive. The port
// compares the value itself, so a negative constant, which the original reports beside <= < >= >, and an untyped
// constant or a literal of value zero that is no integer, which it reports beside < == >= and leaves alone beside the
// == of || and the != of &&, each answer as the arithmetic does.
func isRedundant(context *rule.Context, isEqualNil bool, operator token.Token, bound ast.Expr) bool {
	if isEqualNil {
		switch operator {
		case token.EQL, token.LEQ, token.LSS:
		default:
			return false
		}
	} else {
		switch operator {
		case token.EQL, token.GEQ, token.NEQ, token.GTR:
		default:
			return false
		}
	}
	// The original reads k only when it is written as a literal or as one name that names a constant; a selector, a
	// conversion, a negation or any arithmetic is left alone, constant or not.
	switch bound := bound.(type) {
	case *ast.BasicLit:
	case *ast.Ident:
		if _, isConstant := context.TypesInfo.ObjectOf(bound).(*types.Const); !isConstant {
			return false
		}
	default:
		return false
	}
	recorded := context.TypesInfo.Types[bound].Value
	if recorded == nil {
		return false
	}
	// The bound is compared with len's int, so in code that type-checks it is a constant an int represents: an
	// integer, or an untyped float, rune or complex constant of integral value.
	value := constant.ToInt(recorded)
	if value.Kind() != constant.Int {
		return false
	}
	return constant.Compare(constant.MakeInt64(0), operator, value) == isEqualNil
}

// nilKindsOf is how the message names what a nil x is, nil slices, nil maps or nil channels, and whether every type x
// may have is one of those, as the original asks through typeutil.All: one type for an ordinary x, and every term of
// its constraint for a type parameter, the message naming the last term's kind. A pointer, to an array, has a length
// though nil, so the nil check is no repeat.
func nilKindsOf(checkedType types.Type) (string, bool) {
	nilKinds := ""
	isNillable := typeutil.All(checkedType, func(term *types.Term) bool {
		switch term.Type().Underlying().(type) {
		case *types.Slice:
			nilKinds = "nil slices"
			return true
		case *types.Map:
			nilKinds = "nil maps"
			return true
		case *types.Chan:
			nilKinds = "nil channels"
			return true
		default:
			// The original returns false for a pointer and a type parameter and panics on any other type, which only
			// code that fails to type-check reaches, since len and a comparison with nil both take only the kinds
			// above and a pointer to an array; the port declines there instead.
			return false
		}
	})
	return nilKinds, isNillable
}
