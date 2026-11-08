// Package ineffective_bitwise_operation is staticcheck's SA4016, a bitwise and, or or exclusive or of an integer with
// zero, which does nothing useful, written as a literal zero or as a constant defined as iota whose value is zero,
// likely meant as `1 << iota`, ported as a check on syntax that reports through the kit every port of staticcheck
// shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4016/sa4016.go, whose logic it keeps and
// whose messages it reports word for word, over the whole binary expression, where staticcheck's report puts it. It
// prints the operands into its messages with staticcheck_report.Render, finds the file of a constant's declaration
// with staticcheck_report.File where the original asks code.File, asks staticcheck_code.IntegerLiteral where the
// original asks code.IsIntegerLiteral, comparing the value itself, which is all that wrapper adds, and reports through
// staticcheck_report.Report, each the port of the one the original calls, proven against it beside it. It reads the
// syntax enclosing a constant's declaration with staticcheck's go/ast/astutil.PathEnclosingInterval and an expression's
// set of types with its go/types/typeutil, as the original does. Like the original, it judges only a zero on the
// right, so `0 | x` is left alone, and only a constant of this package declared alone as `name = iota`. The registry
// still runs the original, which stays the oracle: the parity tests beside this file run both over the same modules,
// and the two differ nowhere they hold.
package ineffective_bitwise_operation

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
	"honnef.co/go/tools/go/ast/astutil"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA4016",
	Doc: "Flags a bitwise operation with zero, which does nothing useful: staticcheck's SA4016, ported as a check on " +
		"syntax.",
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
		panic("ineffective_bitwise_operation: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.BinaryExpr)(nil)}, func(node ast.Node) {
		binary, isBinary := node.(*ast.BinaryExpr)
		if !isBinary {
			// Invariant: Preorder hands this function only the binary expressions it was asked for.
			panic("ineffective_bitwise_operation: the inspector handed over a node that is not a binary expression")
		}
		Check(context, binary)
	})
	return nil, nil
}

// Check judges one binary expression: an and, or or exclusive or of integers whose right operand is a constant of
// this package defined as iota with the value zero, or a literal zero. Shifts are left alone, since `x << 0` is often
// the first of a pattern, `x << 0, x << 8, x << 16`.
func Check(context *rule.Context, binary *ast.BinaryExpr) {
	isInteger := typeutil.All(context.TypesInfo.TypeOf(binary), func(term *types.Term) bool {
		basic, isBasic := term.Type().Underlying().(*types.Basic)
		return isBasic && basic.Info()&types.IsInteger != 0
	})
	if !isInteger {
		return
	}
	if binary.Op != token.AND && binary.Op != token.OR && binary.Op != token.XOR {
		return
	}
	if name, isName := binary.Y.(*ast.Ident); isName {
		if !isZeroIota(context, name) {
			return
		}
		expression := staticcheck_report.Render(context, binary)
		operand := staticcheck_report.Render(context, binary.Y)
		switch binary.Op {
		case token.AND:
			staticcheck_report.Report(context, binary, fmt.Sprintf(
				"%s always equals 0; %s is defined as iota and has value 0, maybe %s is meant to be 1 << iota?",
				expression, operand, operand))
		case token.OR, token.XOR:
			staticcheck_report.Report(context, binary, fmt.Sprintf(
				"%s always equals %s; %s is defined as iota and has value 0, maybe %s is meant to be 1 << iota?",
				expression, staticcheck_report.Render(context, binary.X), operand, operand))
		}
		return
	}
	literal, isLiteral := staticcheck_code.IntegerLiteral(context, binary.Y)
	if !isLiteral || !constant.Compare(literal.Value, token.EQL, constant.MakeInt64(0)) {
		return
	}
	switch binary.Op {
	case token.AND:
		staticcheck_report.Report(context, binary,
			fmt.Sprintf("%s always equals 0", staticcheck_report.Render(context, binary)))
	case token.OR, token.XOR:
		staticcheck_report.Report(context, binary, fmt.Sprintf("%s always equals %s",
			staticcheck_report.Render(context, binary), staticcheck_report.Render(context, binary.X)))
	}
}

// isZeroIota is whether name is a constant of this package with the value zero, declared alone in its specification
// as `name = iota` with the universe's iota. A constant dot-imported from another package, one declared in a group
// after `name = iota` and taking iota implicitly, and one beside other names, are all left alone.
func isZeroIota(context *rule.Context, name *ast.Ident) bool {
	object, isConstant := context.TypesInfo.ObjectOf(name).(*types.Const)
	if !isConstant || object.Pkg() != context.Package {
		return false
	}
	if value, _ := constant.Int64Val(object.Val()); value != 0 {
		return false
	}
	path, _ := astutil.PathEnclosingInterval(staticcheck_report.File(context, object), object.Pos(), object.Pos())
	if len(path) < 2 {
		return false
	}
	specification, isValue := path[1].(*ast.ValueSpec)
	if !isValue || len(specification.Names) != 1 || len(specification.Values) != 1 {
		return false
	}
	value, isName := specification.Values[0].(*ast.Ident)
	return isName && isUniverseIota(context.TypesInfo.ObjectOf(value))
}

// isUniverseIota is whether object is the predeclared iota, not a constant a package declares under that name.
func isUniverseIota(object types.Object) bool {
	if object.Name() != "iota" {
		return false
	}
	constantObject, isConstant := object.(*types.Const)
	return isConstant && constantObject.Pkg() == nil
}
