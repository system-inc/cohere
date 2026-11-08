// Package redundant_canonical_header_key is staticcheck's S1035, a call of a net/http.Header method that takes a key,
// Add, Del, Get or Set, whose key is http.CanonicalHeaderKey of something, `headers.Get(http.CanonicalHeaderKey(k))`,
// which canonicalizes what the method canonicalizes again, ported as a check on types and syntax that reports through
// the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1035/s1035.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the inner call. The original matches its one query,
// `(CallExpr (Symbol callName@(Or "(net/http.Header).Add" "(net/http.Header).Del" "(net/http.Header).Get"
// "(net/http.Header).Set")) arg@(CallExpr (Symbol "net/http.CanonicalHeaderKey") _):_)`, through staticcheck's pattern
// language; the port asks the same of each call directly, reading pattern/match.go for what each construct of the
// query matches, and analysis/code/visit.go for which calls code.Matches hands it, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere they hold.
package redundant_canonical_header_key

import (
	"go/ast"
	"go/types"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("redundant_canonical_header_key: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1035",
	"S1035",
	"Flags http.CanonicalHeaderKey as the key of a net/http.Header method that canonicalizes it already: "+
		"staticcheck's S1035, ported.",
	&Listener,
)

// Check judges one call: Add, Del, Get or Set of a net/http.Header whose first argument is a call of
// http.CanonicalHeaderKey canonicalizes the key twice, and the fix replaces the inner call with its own argument.
func Check(context *rule.Context, call *ast.CallExpr) {
	method := headerMethodName(context, call.Fun)
	if method == "" {
		return
	}
	// `arg@(...):_` is a list whose head is the first argument and whose tail is any, so the call has one argument or
	// more. A method expression, `http.Header.Get(headers, key)`, passes the header first, which is no call of
	// http.CanonicalHeaderKey, so the key it passes second is never judged, as in the original.
	if len(call.Args) == 0 {
		return
	}
	// The pattern sees through parentheses around the argument and binds the call inside, where the finding and the
	// fix land, so the parentheses stay around the argument the fix leaves.
	canonical, isCall := ast.Unparen(call.Args[0]).(*ast.CallExpr)
	if !isCall || !isCanonicalHeaderKey(context, canonical.Fun) {
		return
	}
	// The call's arguments are `_`, anything, and the fix reads the first: a call of http.CanonicalHeaderKey
	// type-checks only with its one argument, and a package that does not type-check is never analyzed.
	if len(canonical.Args) == 0 {
		return
	}
	staticcheck_report.Report(context, canonical,
		"calling net/http.CanonicalHeaderKey on the 'key' argument of "+method+" is redundant",
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(edit.Fix("Remove call to CanonicalHeaderKey",
			edit.ReplaceWithNode(context.Fset, canonical, canonical.Args[0]))))
}

// headerMethodName is the full name of the method function calls, `(net/http.Header).Add` and so on, as the pattern's
// `(Symbol callName@(Or ...))` binds it, and empty for any other callee. Symbol matches an identifier or a selector,
// parenthesized or not and instantiated or not, whose object is a function whose full name is one of the four, so the
// methods called on a header through an embedding, a conversion or an alias are the methods themselves, and a method
// value held in a variable, an interface's method or a method of another type named alike is not.
func headerMethodName(context *rule.Context, function ast.Expr) string {
	method := functionNamed(context, function)
	if method == nil {
		return ""
	}
	// code.Matches asks the type index for the four methods by path, and the index knows the packages a package
	// imports or reaches through a method, never the package itself, so net/http's own calls of them are never walked.
	if method.Pkg() == context.Package {
		return ""
	}
	// FullName spells a method with its receiver's package path and type, which is net/http.Header for the standard
	// library's Header alone.
	switch method.FullName() {
	case "(net/http.Header).Add", "(net/http.Header).Del", "(net/http.Header).Get", "(net/http.Header).Set":
		return method.FullName()
	default:
		return ""
	}
}

// isCanonicalHeaderKey is whether function names http.CanonicalHeaderKey, as the pattern's `(Symbol
// "net/http.CanonicalHeaderKey")` matches it, so a dot import and a renamed import name it, and a function of the
// package's own named alike, or a variable holding it, does not. Symbol also names a package's variable, constant or
// type by its package's path and name, and none spelled net/http.CanonicalHeaderKey exists, since only the standard
// library's net/http has that path and it declares CanonicalHeaderKey as a function alone. The type index's guard for
// net/http's own package is the outer method's, whose package is the same.
func isCanonicalHeaderKey(context *rule.Context, function ast.Expr) bool {
	canonical := functionNamed(context, function)
	return canonical != nil && canonical.FullName() == "net/http.CanonicalHeaderKey"
}

// functionNamed is the function function names, as the pattern's Symbol finds its object: the expression without its
// parentheses or its instantiation, itself parenthesized or not, an identifier or a selector, whose object is a
// function or a method. It is nil for any other expression or object.
func functionNamed(context *rule.Context, function ast.Expr) *types.Func {
	name := ast.Unparen(function)
	switch instantiated := name.(type) {
	case *ast.IndexExpr:
		name = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		name = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch name := name.(type) {
	case *ast.Ident:
		object = context.TypesInfo.ObjectOf(name)
	case *ast.SelectorExpr:
		object = context.TypesInfo.ObjectOf(name.Sel)
	default:
		return nil
	}
	named, isFunction := object.(*types.Func)
	if !isFunction {
		return nil
	}
	return named
}
