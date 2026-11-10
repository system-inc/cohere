// Package errors_new_of_sprintf is staticcheck's S1028, an error made by errors.New of a message fmt.Sprintf formats,
// `errors.New(fmt.Sprintf(format, arguments...))`, which is `fmt.Errorf(format, arguments...)` written longer, ported
// as a check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1028/s1028.go, whose logic it keeps and whose
// message it reports word for word, at the same range, the call of errors.New. The original matches its one query,
// `(CallExpr (Symbol "errors.New") [(CallExpr (Symbol "fmt.Sprintf") args)])`, through staticcheck's pattern
// language, and builds its fix from another, `(CallExpr (SelectorExpr (Ident "fmt") (Ident "Errorf")) args)`, through
// code.EditMatch, which is edit.ReplaceWithPattern by another name. The port asks the same of each call directly,
// reading pattern/match.go for what each construct of the query matches, and analysis/code/visit.go for which calls
// code.Matches hands it, builds the fix's syntax itself around Sprintf's own arguments, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ only where the original's fix writes broken code, each case recorded
// in the differences fixture: the port keeps a spread's ellipsis the original drops, moves the fix to the name the code
// itself calls fmt.Sprintf by where fmt names no import of fmt, and withholds the fix where no name it could write
// calls fmt.Errorf.
package errors_new_of_sprintf

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
			panic("errors_new_of_sprintf: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1028",
	"S1028",
	"Flags errors.New of a message fmt.Sprintf formats: staticcheck's S1028, ported.",
	&Listener,
)

// Check judges one call: errors.New of exactly one argument, a call of fmt.Sprintf, makes the error fmt.Errorf makes
// from the same arguments, and the fix calls fmt.Errorf on Sprintf's own arguments, over the whole call of errors.New,
// wherever some name the code can write there calls fmt.Errorf.
func Check(context *rule.Context, call *ast.CallExpr) {
	if !isCallOf(context, call.Fun, "errors.New") {
		return
	}
	// `[(CallExpr ...)]` is a list of one, so errors.New has exactly one argument.
	if len(call.Args) != 1 {
		return
	}
	// The pattern sees through parentheses around the argument, and matches the call inside.
	sprintf, isCall := ast.Unparen(call.Args[0]).(*ast.CallExpr)
	if !isCall || !isCallOf(context, sprintf.Fun, "fmt.Sprintf") {
		return
	}

	fixes := []analysis.SuggestedFix{}
	if spelled, isSpelled := errorfFunction(context, call, sprintf.Fun); isSpelled {
		// `args` binds Sprintf's own argument slice, which the fix's call holds, the same nodes, not copies, as
		// pattern.NodeToAST places a binding's value, so format prints them from their own positions. The pattern's
		// CallExpr has no ellipsis, so the original's call has none, and its fix of `fmt.Sprintf(format, list...)` is
		// `fmt.Errorf(format, list)`, which type-checks and formats the list as one value. The port's call keeps
		// Sprintf's ellipsis, at its own position, which is the original's fix to the byte wherever Sprintf has none.
		errorf := &ast.CallExpr{Fun: spelled, Args: sprintf.Args, Ellipsis: sprintf.Ellipsis}
		// code.EditMatch drops format.Node's error where edit.ReplaceWithNode panics on it, and neither meets one,
		// since every node printed is the package's own parsed syntax or an identifier.
		fixes = append(fixes, edit.Fix("Use fmt.Errorf", edit.ReplaceWithNode(context.FileSet, call, errorf)))
	}
	staticcheck_report.Report(context, call, "should use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))",
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(fixes...))
}

// errorfFunction is how the fix names fmt.Errorf where the call of errors.New stands, and whether any name does. The
// original writes `fmt.Errorf` always, the pattern's `(Ident "fmt")`, which fails to compile, or calls something else,
// where fmt names no import of the package fmt there: a file that imports it under another name or into its own
// scope, or a scope that declares a fmt of its own. The port writes `fmt.Errorf` wherever fmt names that import, which
// is the original's fix to the byte, and elsewhere moves the fix to the name the code itself calls fmt.Sprintf by,
// which stands in the same scope, since the call of Sprintf is errors.New's argument: the package name of a selector,
// `formatting.Errorf`, or for a dot import Errorf alone, where Errorf is fmt's. Where neither names it, a dot import
// whose Errorf a nearer scope declares an Errorf over, the port keeps the finding and withholds the fix. Each case is
// recorded in the differences fixture.
func errorfFunction(context *rule.Context, call *ast.CallExpr, sprintfFunction ast.Expr) (ast.Expr, bool) {
	scope := context.Package.Scope().Innermost(call.Pos())
	if scope == nil {
		// Invariant: every call stands in a file, whose scope the type checker records around the file's whole extent.
		panic("errors_new_of_sprintf: no scope holds a call")
	}
	// The call's position counts only what is declared before it in its own block, as the call's place does.
	if _, named := scope.LookupParent("fmt", call.Pos()); importsFmt(named) {
		return edit.Selector("fmt", "Errorf"), true
	}
	// isCallOf has already seen through the function's parentheses, and fmt.Sprintf takes no type arguments.
	switch named := ast.Unparen(sprintfFunction).(type) {
	case *ast.SelectorExpr:
		// A selector that names a package's function selects it from the package's name, an identifier.
		qualifier, isIdentifier := named.X.(*ast.Ident)
		if isIdentifier && importsFmt(context.TypesInfo.Uses[qualifier]) {
			return edit.Selector(qualifier.Name, "Errorf"), true
		}
	case *ast.Ident:
		// An identifier alone names fmt.Sprintf only through a dot import, which brings fmt.Errorf into the file's
		// scope as Errorf, unless a scope nearer the call declares an Errorf of its own.
		sprintf := context.TypesInfo.Uses[named]
		if sprintf == nil || sprintf.Pkg() == nil {
			return nil, false
		}
		_, errorf := scope.LookupParent("Errorf", call.Pos())
		if errorf != nil && errorf == context.LookupIn(sprintf.Pkg(), "Errorf") {
			return &ast.Ident{Name: "Errorf"}, true
		}
	}
	return nil, false
}

// importsFmt is whether object is the name of an import of the standard library's package fmt.
func importsFmt(object types.Object) bool {
	packageName, isPackageName := object.(*types.PkgName)
	return isPackageName && packageName.Imported().Path() == "fmt"
}

// isCallOf is whether function names the function fullName names, errors.New or fmt.Sprintf, as the pattern's
// `(Symbol "...")` matches it: an identifier or a selector, parenthesized or not and instantiated or not, whose object
// is a function of that full name, so a dot import and a renamed import name it, and a method, a variable holding the
// function, or a function of another package named alike does not. Symbol also names a package's variable, constant
// or type by its package's path and name, and none spelled either name exists, since only the standard library's
// errors and fmt have those paths and each declares its name as a function alone.
func isCallOf(context *rule.Context, function ast.Expr, fullName string) bool {
	// The name is the function without its parentheses or its instantiation, itself parenthesized or not.
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
		return false
	}
	called, isFunction := object.(*types.Func)
	if !isFunction {
		return false
	}
	// code.Matches first asks code.CouldMatchAny whether the type index knows both errors.New and fmt.Sprintf, an
	// And of the query's two symbols, and the index knows the packages a package imports, never the package itself,
	// so inside errors or inside fmt it knows one of the two not at all and the original walks nothing. Skipping a
	// callee of the package under analysis skips the same: inside errors every call of errors.New is its own, and
	// inside fmt every call of fmt.Sprintf is, and a finding needs both.
	if called.Pkg() == context.Package {
		return false
	}
	// The full name Matches compares spells a method with its receiver, `(T).New`, and a function by its package's
	// path.
	return function_name.Matches(called, fullName)
}
