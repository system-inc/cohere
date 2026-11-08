// Package extreme_comparison is staticcheck's SA4003, a comparison of an integer with the least or greatest value of
// its type, such as `x < 0` for an unsigned x or `x > math.MaxInt8` for an int8, which is always false or always true,
// ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4003/sa4003.go, whose logic it keeps and
// whose messages it reports word for word, over the whole comparison, in files staticcheck does not judge generated,
// as its report.FilterGenerated has it. It asks staticcheck_code.IntegerLiteral where the original asks
// code.IsIntegerLiteral, comparing the value itself, which is all that wrapper adds, reads a type's set of types and a
// constant's name through staticcheck's go/types/typeutil, as the original does, and reports through
// staticcheck_report.Report, each the port of the one the original calls, proven against it beside it. Like the
// original, it knows only the math package's limits and integer literals, never other constant expressions, whose
// values can differ under build tags, and it takes int and uint to be 64 bits wide whatever the target. The registry
// still runs the original, which stays the oracle: the parity tests beside this file run both over the same packages,
// and the two differ nowhere they hold.
package extreme_comparison

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"

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
// generated pass, which the context's judgment of generated files reads.
var Analyzer = &analysis.Analyzer{
	Name: "SA4003",
	Doc: "Flags a comparison with the least or greatest value of an integer type, which never changes: " +
		"staticcheck's SA4003, ported as a check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// limits are the least and greatest values of one integer type, as the math package names them and as literals. A
// name is empty, and a literal nil, where the type has no such limit the check reads, as an unsigned type has no
// named least value.
type limits struct {
	leastName     string
	greatestName  string
	leastValue    constant.Value
	greatestValue constant.Value
}

// limitsOf are the limits of each integer kind. int and uint are taken as 64 bits wide.
var limitsOf = map[types.BasicKind]limits{
	types.Uint8:  {"", "math.MaxUint8", constant.MakeUint64(0), constant.MakeUint64(math.MaxUint8)},
	types.Uint16: {"", "math.MaxUint16", constant.MakeUint64(0), constant.MakeUint64(math.MaxUint16)},
	types.Uint32: {"", "math.MaxUint32", constant.MakeUint64(0), constant.MakeUint64(math.MaxUint32)},
	types.Uint64: {"", "math.MaxUint64", constant.MakeUint64(0), constant.MakeUint64(math.MaxUint64)},
	types.Uint:   {"", "math.MaxUint64", constant.MakeUint64(0), constant.MakeUint64(math.MaxUint64)},
	types.Int8: {
		"math.MinInt8", "math.MaxInt8", constant.MakeInt64(math.MinInt8), constant.MakeInt64(math.MaxInt8),
	},
	types.Int16: {
		"math.MinInt16", "math.MaxInt16", constant.MakeInt64(math.MinInt16), constant.MakeInt64(math.MaxInt16),
	},
	types.Int32: {
		"math.MinInt32", "math.MaxInt32", constant.MakeInt64(math.MinInt32), constant.MakeInt64(math.MaxInt32),
	},
	types.Int64: {
		"math.MinInt64", "math.MaxInt64", constant.MakeInt64(math.MinInt64), constant.MakeInt64(math.MaxInt64),
	},
	types.Int: {
		"math.MinInt64", "math.MaxInt64", constant.MakeInt64(math.MinInt64), constant.MakeInt64(math.MaxInt64),
	},
}

// zero is the literal an unsigned value is never less than.
var zero = constant.MakeInt64(0)

// run builds the pass's context and judges every binary expression of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("extreme_comparison: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.BinaryExpr)(nil)}, func(node ast.Node) {
		binary, isBinary := node.(*ast.BinaryExpr)
		if !isBinary {
			// Invariant: Preorder hands this function only the binary expressions it was asked for.
			panic("extreme_comparison: the inspector handed over a node that is not a binary expression")
		}
		Check(context, binary)
	})
	return nil, nil
}

// Check judges one binary expression: an unsigned value compared with a literal zero, and an integer whose type set
// has one core type compared with that type's least or greatest value, by the math package's name or as a literal.
func Check(context *rule.Context, binary *ast.BinaryExpr) {
	leftType := context.TypesInfo.TypeOf(binary.X)
	typeSet := typeutil.NewTypeSet(leftType)
	allUnsigned := typeSet.All(func(term *types.Term) bool {
		basic, isBasic := term.Type().Underlying().(*types.Basic)
		return isBasic && basic.Info()&types.IsUnsigned != 0
	})

	if allUnsigned {
		if (binary.Op == token.LSS && isIntegerLiteral(context, binary.Y, zero)) ||
			(binary.Op == token.GTR && isIntegerLiteral(context, binary.X, zero)) {
			report(context, binary, fmt.Sprintf("no value of type %s is less than 0", leftType))
		}
		if (binary.Op == token.GEQ && isIntegerLiteral(context, binary.Y, zero)) ||
			(binary.Op == token.LEQ && isIntegerLiteral(context, binary.X, zero)) {
			report(context, binary, fmt.Sprintf("every value of type %s is >= 0", leftType))
		}
	}

	// The rest needs one underlying type for the whole type set: with `uint8 | uint16` the type checker would refuse
	// a comparison with math.MaxUint16 anyway, since the constant does not fit every type.
	core, isBasic := typeSet.CoreType().(*types.Basic)
	if !isBasic {
		return
	}
	// A basic kind with no limits here, a float or a string, has every name empty and every literal nil, so nothing
	// below matches it.
	bounds := limitsOf[core.Kind()]

	// Every comparison is turned to `greater >= lesser` or `greater > lesser` by swapping the operands of <= and <.
	greater, lesser, operator := binary.X, binary.Y, binary.Op
	switch operator {
	case token.GEQ, token.GTR:
	case token.LEQ:
		greater, lesser = lesser, greater
		operator = token.GEQ
	case token.LSS:
		greater, lesser = lesser, greater
		operator = token.GTR
	default:
		return
	}

	if isLimit(context, lesser, bounds.greatestName, bounds.greatestValue) {
		report(context, binary, fmt.Sprintf("no value of type %s is greater than %s", leftType, bounds.greatestName))
	}
	if operator == token.GEQ && isLimit(context, greater, bounds.greatestName, bounds.greatestValue) {
		report(context, binary, fmt.Sprintf("every value of type %s is <= %s", leftType, bounds.greatestName))
	}
	if !allUnsigned {
		if isLimit(context, greater, bounds.leastName, bounds.leastValue) {
			report(context, binary, fmt.Sprintf("no value of type %s is less than %s", leftType, bounds.leastName))
		}
		if operator == token.GEQ && isLimit(context, lesser, bounds.leastName, bounds.leastValue) {
			report(context, binary, fmt.Sprintf("every value of type %s is >= %s", leftType, bounds.leastName))
		}
	}
}

// report reports message over binary in a file staticcheck does not judge generated.
func report(context *rule.Context, binary *ast.BinaryExpr, message string) {
	staticcheck_report.Report(context, binary, message, staticcheck_report.FilterGenerated())
}

// isLimit is whether expression names the math package's constant of the qualified name, through a selector such as
// `math.MaxInt8`, or is an integer literal of value. An empty name and a nil value match nothing.
func isLimit(context *rule.Context, expression ast.Expr, name string, value constant.Value) bool {
	if name != "" {
		if selector, isSelector := expression.(*ast.SelectorExpr); isSelector &&
			typeutil.IsObject(context.TypesInfo.ObjectOf(selector.Sel), name) {
			return true
		}
	}
	return value != nil && isIntegerLiteral(context, expression, value)
}

// isIntegerLiteral is whether expression is an integer literal, under any signs and parentheses, of value:
// staticcheck's code.IsIntegerLiteral, which asks IntegerLiteral and compares the value it recorded.
func isIntegerLiteral(context *rule.Context, expression ast.Expr, value constant.Value) bool {
	typeAndValue, isLiteral := staticcheck_code.IntegerLiteral(context, expression)
	return isLiteral && constant.Compare(typeAndValue.Value, token.EQL, value)
}
