// Package time_since is staticcheck's S1012, the time now less a time, `time.Now().Sub(t)`, which is `time.Since(t)`
// written longer, ported as a check on types and syntax that reports through the kit every port of staticcheck
// shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1012/s1012.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the call. The original matches its one query,
// `(CallExpr (SelectorExpr (CallExpr (Symbol "time.Now") []) (Symbol "(time.Time).Sub")) [arg])`, through
// staticcheck's pattern language, and builds its fix from a second, `(CallExpr (SelectorExpr (Ident "time") (Ident
// "Since")) [arg])`, through code.EditMatch, which is pattern.NodeToAST over the match's bindings. The port asks the
// same of each call directly, reading pattern/match.go for what each construct of the query matches, builds the fix's
// syntax itself as pattern.NodeToAST builds it, around the argument the match binds, and reports through
// staticcheck_report.Report where the original reports through report.Report. time.Since is as old as Go 1, so
// neither gates on a version. The registry runs the port in the original's place, under its name, and the original
// stays only as the oracle: the parity tests beside this file run both over the same packages, and the two differ only
// where the original's fix names the package time by a name the code does not give it, each case recorded in the
// differences fixture: the port moves the fix to the name the matched call of time.Now uses, and withholds the fix
// where no name it could write calls time.Since.
package time_since

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/function_name"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("time_since: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1012",
	"S1012",
	"Flags the time now less a time, in place of time.Since: staticcheck's S1012, ported.",
	&Listener,
)

// Check judges one call: the method Sub of time.Time, selected on a call of time.Now with no arguments and called on
// one argument, asks what time.Since answers, and the fix calls time.Since on that argument, by whatever name names
// it where the call stands, and is withheld where none does.
func Check(context *rule.Context, call *ast.CallExpr) {
	// code.Matches first asks the type index whether both of the query's symbols could be named, and the index keeps
	// no symbol of the package it indexes, so in the time package the query is never tried. Anywhere else a call of
	// time.Now names the package time, which puts it in the index, so the question decides nothing there.
	if context.Package.Path() == "time" {
		return
	}
	// `[arg]` is a list of one, and the pattern sees through parentheses around the callee, `(time.Now().Sub)(t)`.
	if len(call.Args) != 1 {
		return
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector || !isFunction(context, selector.Sel, "(time.Time).Sub") {
		return
	}
	// The receiver is `(CallExpr (Symbol "time.Now") [])`, seen through parentheses, and `[]` is a call with no
	// arguments: a method expression, `time.Time.Sub(time.Now(), t)`, selects from a type and is not the shape.
	now, isCall := ast.Unparen(selector.X).(*ast.CallExpr)
	if !isCall || len(now.Args) != 0 || !isFunction(context, now.Fun, "time.Now") {
		return
	}

	fixes := []analysis.SuggestedFix{}
	if function, isSpelled := sinceFunction(context, call, now.Fun); isSpelled {
		// `arg` binds what the match reaches once it has looked through every parenthesis around the argument, so the
		// fix holds `t` for `time.Now().Sub((t))`, the same node, not a copy, as pattern.NodeToAST places a binding's
		// value, so format prints it from its own positions. The argument already fits time.Since: it is passed to
		// (time.Time).Sub, whose parameter is time.Time as Since's is.
		since := &ast.CallExpr{Fun: function, Args: []ast.Expr{ast.Unparen(call.Args[0])}}
		fixes = append(fixes,
			edit.Fix("Replace with call to time.Since", edit.ReplaceWithNode(context.FileSet, call, since)))
	}
	staticcheck_report.Report(context, call, "should use time.Since instead of time.Now().Sub",
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(fixes...))
}

// sinceFunction is how the fix names time.Since where the call stands, and whether any name does. The original writes
// `time.Since` always, the pattern's `(Ident "time")`, which fails to compile, or calls something else, where time
// names no import of the package time there: a file that imports it under another name or into its own scope alone,
// or a scope that declares a time of its own. The port writes `time.Since` wherever time names that import, which is
// the original's fix to the byte, and elsewhere moves the fix to the name the call itself gives time.Now, which
// stands in the same scope: the package name of a selector, `clock.Since`, or for a dot import Since alone, where
// Since is time's. Where neither names it, a dot import beside a Since the code declares over time's, the port keeps
// the finding and withholds the fix. Each case is recorded in the differences fixture.
func sinceFunction(context *rule.Context, call *ast.CallExpr, nowFunction ast.Expr) (ast.Expr, bool) {
	// A call stands in a function's body or in a package-level declaration, and the package scope answers Innermost
	// through its files' scopes, so a call of either place has a scope.
	scope := context.Package.Scope().Innermost(call.Pos())
	if scope == nil {
		// Invariant: the type checker records a scope around every file it checks, and every call stands in a file.
		panic("time_since: no scope holds a call")
	}
	// The call's position counts only what is declared before it in its own block, as the call's place does.
	if _, named := scope.LookupParent("time", call.Pos()); importsTime(named) {
		return edit.Selector("time", "Since"), true
	}
	switch named := ast.Unparen(nowFunction).(type) {
	case *ast.SelectorExpr:
		// A selector that names a package's function selects it from the package's name, an identifier, which names
		// the package where the call stands, since the call of time.Now stands inside the call the fix replaces.
		qualifier, isIdentifier := named.X.(*ast.Ident)
		if isIdentifier && importsTime(context.TypesInfo.Uses[qualifier]) {
			return edit.Selector(qualifier.Name, "Since"), true
		}
	case *ast.Ident:
		// An identifier alone names time.Now only through a dot import, which brings time.Since into the file's scope
		// as Since, unless a scope nearer the call declares a Since of its own.
		now := context.TypesInfo.Uses[named]
		if now == nil || now.Pkg() == nil {
			return nil, false
		}
		_, since := scope.LookupParent("Since", call.Pos())
		if since != nil && since == context.LookupIn(now.Pkg(), "Since") {
			return &ast.Ident{Name: "Since"}, true
		}
	}
	return nil, false
}

// importsTime is whether object is the name of an import of the standard library's package time.
func importsTime(object types.Object) bool {
	packageName, isPackageName := object.(*types.PkgName)
	return isPackageName && packageName.Imported().Path() == "time"
}

// isFunction is whether function names the function or method of fullName, as the pattern's `(Symbol fullName)`
// matches it: an identifier or a selector, parenthesized or not, whose object is a function whose types.Func.FullName
// is fullName, so a dot import and a renamed import name time.Now, and a function of this package spelled Now, a
// variable holding time.Now and a struct's field do not. Symbol also looks through an instantiation, which neither
// name can have, since time.Now has no type parameters and a method declares none of its own. Symbol also names a
// package's variable, constant or type by its package's path and name, and none is spelled either way, since only the
// standard library's time has the path time, and it declares Now as a function and Sub as a method alone.
func isFunction(context *rule.Context, function ast.Expr, fullName string) bool {
	var object types.Object
	switch name := ast.Unparen(function).(type) {
	case *ast.Ident:
		object = context.TypesInfo.ObjectOf(name)
	case *ast.SelectorExpr:
		object = context.TypesInfo.ObjectOf(name.Sel)
	default:
		return false
	}
	// The full name Matches compares spells a method with its receiver, `(time.Time).Sub`, and a function by its
	// package's path.
	named, isFunction := object.(*types.Func)
	return isFunction && function_name.Matches(named, fullName)
}
