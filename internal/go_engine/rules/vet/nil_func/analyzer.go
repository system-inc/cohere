// Package nil_func is vet's nilfunc pass, a comparison of a function with nil, which is always false with `==` and
// always true with `!=`, since a declared function or method is never nil. A variable or field of function type may
// be nil, so only a name that denotes a function itself is judged. It sits on the engine's syntax dispatcher, hearing
// each binary expression on the walk the dispatcher makes once for every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/nilfunc/nilfunc.go, whose logic
// it keeps and whose messages it reports word for word, at the same position. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture.
package nil_func

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "nilfunc"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for useless comparisons between functions and nil\n\n" +
	"A useless comparison is one like f == nil as opposed to f() == nil."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/nilfunc"

// Listener is what the port hears of each package from the dispatcher: every binary expression, in preorder, as the
// original's Preorder visits them, of which it judges the comparisons with nil.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.BinaryExpr)(nil)},
	Node:      checkComparison,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	return analyzer
}

// checkComparison reports node when it compares with nil, by `==` or `!=`, a name that denotes a function itself.
func checkComparison(context *rule.Context, node ast.Node, _ []ast.Node) {
	comparison, isBinary := node.(*ast.BinaryExpr)
	if !isBinary || (comparison.Op != token.EQL && comparison.Op != token.NEQ) {
		return
	}
	// The side compared with the predeclared nil. When both sides are nil, the comparison does not type-check.
	var compared ast.Expr
	switch {
	case context.TypesInfo.Types[comparison.X].IsNil():
		compared = comparison.Y
	case context.TypesInfo.Types[comparison.Y].IsNil():
		compared = comparison.X
	default:
		return
	}
	// A function variable or field is a *types.Var, and may be nil, so only a declared function or method, or a
	// method value or expression, which Uses records as its *types.Func, is always non-nil. Uses holds nothing for a
	// nil identifier, so a side that names no identifier is left alone.
	function, isFunction := context.TypesInfo.Uses[usedIdentifier(context.TypesInfo, compared)].(*types.Func)
	if !isFunction {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos: comparison.Pos(),
		End: comparison.End(),
		Message: fmt.Sprintf(
			"comparison of function %s %s nil is always %t",
			function.Name(), comparison.Op, comparison.Op == token.NEQ,
		),
	})
}

// usedIdentifier is the identifier whose use expression denotes, looking through parentheses and an instantiation of
// a generic function, `function[int]`, and nil when expression is neither an identifier nor a selector. x/tools'
// typesinternal.UsedIdent, less its panic when information lacks Types or Uses, which a pass always fills.
func usedIdentifier(information *types.Info, expression ast.Expr) *ast.Ident {
	// uninstantiated is expression, or, when it instantiates a generic function, that function without its type
	// arguments.
	uninstantiated := expression
	switch instantiated := ast.Unparen(expression).(type) {
	case *ast.IndexExpr:
		// `values[index]` is an index into a value, not an instantiation, and is judged as written.
		if information.Types[instantiated.Index].IsType() {
			uninstantiated = instantiated.X
		}
	case *ast.IndexListExpr:
		uninstantiated = instantiated.X
	}
	switch used := ast.Unparen(uninstantiated).(type) {
	// Uses records the object a selector names under its Sel, for a qualified identifier, a method value and a
	// method expression alike, so Selections is never needed.
	case *ast.Ident:
		return used
	case *ast.SelectorExpr:
		return used.Sel
	}
	return nil
}
