// Package ineffective_copy is staticcheck's SA4001, `&*x` or `*&x`, which Go simplifies to x and so copies nothing,
// ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4001/sa4001.go, whose logic it keeps and
// whose messages it reports word for word, over the same range, the whole expression. The original matches two
// patterns of staticcheck's pattern package, `(UnaryExpr "&" (StarExpr obj))` and `(StarExpr (UnaryExpr "&" _))`,
// at every unary and star expression; the port tests the same shapes by their syntax, looking through parentheses
// between the two operators as the pattern does, so `&(*x)` and `*(&x)` are found too, and reading the operand
// spared for cgo, `&*_Cvar_name`, through parentheses as the pattern binds it. Neither pattern names a symbol, so the
// original walks every package, and so does the port. It reports through staticcheck_report.Report where the
// original reports through report.Report, with no option, so a generated file is judged like any other. The registry
// still runs the original, which stays the oracle: the parity tests beside this file run both over the same packages,
// and the two differ nowhere they hold.
package ineffective_copy

import (
	"go/ast"
	"go/token"
	"regexp"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// cgoIdentifier is the name cgo gives the variables and functions it generates, which its code passes as
// `fn(&*_Cvar_name)`, an ineffective copy no one wrote.
var cgoIdentifier = regexp.MustCompile(`^_C(func|var)_.+$`)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA4001",
	Doc: "Flags `&*x` and `*&x`, which Go simplifies to x and which copy nothing: staticcheck's SA4001, ported as a " +
		"check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every unary and star expression of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("ineffective_copy: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.UnaryExpr)(nil), (*ast.StarExpr)(nil)}, func(node ast.Node) {
		expression, isExpression := node.(ast.Expr)
		if !isExpression {
			// Invariant: Preorder hands this function only the unary and star expressions it was asked for.
			panic("ineffective_copy: the inspector handed over a node that is not an expression")
		}
		Check(context, expression)
	})
	return nil, nil
}

// Check judges one unary or star expression: the address of a dereference, unless what is dereferenced is a name cgo
// generated, or the dereference of an address.
func Check(context *rule.Context, expression ast.Expr) {
	switch outer := expression.(type) {
	case *ast.UnaryExpr:
		if outer.Op != token.AND {
			return
		}
		dereference, isDereference := ast.Unparen(outer.X).(*ast.StarExpr)
		if !isDereference {
			return
		}
		identifier, isIdentifier := ast.Unparen(dereference.X).(*ast.Ident)
		if isIdentifier && cgoIdentifier.MatchString(identifier.Name) {
			return
		}
		staticcheck_report.Report(context, outer, "&*x will be simplified to x. It will not copy x.")
	case *ast.StarExpr:
		address, isAddress := ast.Unparen(outer.X).(*ast.UnaryExpr)
		if !isAddress || address.Op != token.AND {
			return
		}
		staticcheck_report.Report(context, outer, "*&x will be simplified to x. It will not copy x.")
	}
}
