// Package time_until is staticcheck's S1024, a time subtracted from the time now, `t.Sub(time.Now())`, which is
// `time.Until(t)` written longer, ported as a check on types and syntax that reports through the kit every port of
// staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1024/s1024.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the call. The original matches its one query,
// `(CallExpr (Symbol "(time.Time).Sub") [(CallExpr (Symbol "time.Now") [])])`, through staticcheck's pattern language,
// walking the calls the type index holds of (time.Time).Sub, and builds its fix from a second, `(CallExpr
// (SelectorExpr (Ident "time") (Ident "Until")) [arg])`, through pattern.NodeToAST. The port asks the same of each
// call directly, reading pattern/match.go for what each construct of the query matches, builds the fix's syntax itself
// as pattern.NodeToAST builds it, around the receiver as written, and reports through staticcheck_report.Report where
// the original reports through report.Report, behind the same gate: time.Until is go1.8's, so a file whose standard
// library is older is told nothing. The registry runs the port in the original's place, under its name, and the
// original stays only as the oracle: the parity tests beside this file run both over the same packages, under go1.7 and
// go1.8 alike, and the two make the same findings everywhere. Their fixes part only where the original's writes code
// that does not compile, each case recorded in the differences fixture: where time names no import of the package time
// at the call, the port names time.Until as the call names time.Now, and where the receiver is not a time.Time, a
// pointer to one or a struct that embeds one, the port hands time.Until the time.Time itself. Where no spelling reaches
// either, the port keeps the finding and withholds the fix.
package time_until

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
			panic("time_until: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1024",
	"S1024",
	"Flags a time subtracted from the time now, in place of time.Until: staticcheck's S1024, ported.",
	&Listener,
)

// message is the original's, in both of its branches.
const message = "should use time.Until instead of t.Sub(time.Now())"

// Check judges one call: the method Sub of time.Time called on one argument, a call of time.Now with none, asks what
// time.Until answers, and where the method is selected as written, the fix calls time.Until on the time.Time the
// method is called on, by the names the code gives time and the time (see untilFunction and receivedTime).
func Check(context *rule.Context, call *ast.CallExpr) {
	// The original walks the type index's calls of the symbols its query names, and the index keeps no symbol of the
	// package it indexes, so the time package's own calls are never walked.
	if context.Package.Path() == "time" {
		return
	}
	// `[(CallExpr ...)]` is a list of one: a method expression, `time.Time.Sub(t, time.Now())`, passes the receiver
	// as a second argument, and is not the query's shape.
	if len(call.Args) != 1 || !isFunction(context, call.Fun, "(time.Time).Sub") {
		return
	}
	// The pattern sees through parentheses around the argument, and `[]` is a call with no arguments.
	now, isCall := ast.Unparen(call.Args[0]).(*ast.CallExpr)
	if !isCall || len(now.Args) != 0 || !isFunction(context, now.Fun, "time.Now") {
		return
	}

	// The original fixes only a callee that is a selector as written: a parenthesized one, `(t.Sub)(time.Now())`,
	// matches the query, which sees through the parentheses, and is reported with no fix.
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		staticcheck_report.Report(context, call, message,
			staticcheck_report.MinimumStandardLibraryVersion("go1.8"),
			staticcheck_report.FilterGenerated())
		return
	}
	fixes := []analysis.SuggestedFix{}
	function, isNamed := untilFunction(context, call, now.Fun)
	argument, isReached := receivedTime(context, selector)
	if isNamed && isReached {
		until := &ast.CallExpr{Fun: function, Args: []ast.Expr{argument}}
		fixes = append(fixes, edit.Fix("Replace with call to time.Until",
			edit.ReplaceWithNode(context.FileSet, call, until)))
	}
	staticcheck_report.Report(context, call, message,
		staticcheck_report.FilterGenerated(),
		staticcheck_report.MinimumStandardLibraryVersion("go1.8"),
		staticcheck_report.Fixes(fixes...))
}

// untilFunction is how the fix names time.Until where the call stands, and whether any name does. The original writes
// `time.Until` always, the pattern's `(Ident "time")`, which fails to compile where time names no import of the
// package time there: a file that imports it under another name or into its own scope alone, or a scope that declares
// a time of its own, such as a variable holding a time. The port writes `time.Until` wherever time names that import,
// which is the original's fix to the byte, and elsewhere moves the fix to the name the call itself gives time.Now,
// which stands in the same scope: the package name of a selector, `clock.Until`, or for a dot import Until alone,
// where Until is time's. Where neither names it, a dot import whose Until a nearer scope declares over, the port
// keeps the finding and withholds the fix.
func untilFunction(context *rule.Context, call *ast.CallExpr, nowFunction ast.Expr) (ast.Expr, bool) {
	scope := context.Package.Scope().Innermost(call.Pos())
	if scope == nil {
		// Invariant: a call stands in one of the package's files, whose scope the type checker records around it.
		panic("time_until: no scope holds a call")
	}
	// The call's position counts only what is declared before it in its own block, as the call's place does.
	if _, named := scope.LookupParent("time", call.Pos()); importsTime(named) {
		return edit.Selector("time", "Until"), true
	}
	switch named := ast.Unparen(nowFunction).(type) {
	case *ast.SelectorExpr:
		// A selector that names a package's function selects it from the package's name, an identifier.
		qualifier, isIdentifier := named.X.(*ast.Ident)
		if isIdentifier && importsTime(context.TypesInfo.Uses[qualifier]) {
			return edit.Selector(qualifier.Name, "Until"), true
		}
	case *ast.Ident:
		// An identifier alone names time.Now only through a dot import, which brings time.Until into the file's
		// scope as Until, unless a scope nearer the call declares an Until of its own.
		now := context.TypesInfo.Uses[named]
		if now == nil || now.Pkg() == nil {
			return nil, false
		}
		_, until := scope.LookupParent("Until", call.Pos())
		if until != nil && until == context.LookupIn(now.Pkg(), "Until") {
			return &ast.Ident{Name: "Until"}, true
		}
	}
	return nil, false
}

// receivedTime is the time.Time the fix hands time.Until, and whether the code can spell it. The original hands it
// the selector's receiver as written, which is the time.Time itself only where the receiver is one: through a
// pointer, the method is the same and the receiver is a pointer, and through an embedded field, the method is
// promoted and the receiver is the struct that embeds it, and time.Until takes neither. The port hands it the
// receiver as written where that is a time.Time, the receiver's own node, not a copy, as pattern.NodeToAST places a
// binding's value, so format prints it from its own positions, its parentheses kept, which is the original's fix to
// the byte. Elsewhere it moves the fix to the time.Time the method is called on: the embedded field selected on the
// receiver, by its name alone where that name selects it there, `embedded.Time`, and otherwise by every embedded field
// on the way, `outer.inner.Time`, where the package may name each, and the time.Time a pointer points at, `*pointer`.
// Where no selector reaches the field, the port keeps the finding and withholds the fix.
func receivedTime(context *rule.Context, selector *ast.SelectorExpr) (ast.Expr, bool) {
	selection := context.TypesInfo.Selections[selector]
	if selection == nil || selection.Kind() != types.MethodVal {
		// Invariant: a call of time.Time's Sub on one argument selects the method from a value, since a method
		// expression passes its receiver as a second argument, and the type checker records every selection.
		panic("time_until: the call of Sub selects no method from a value")
	}
	receiverType := context.TypesInfo.TypeOf(selector.X)
	// The selection's path is every embedded field on the way to the method, then the method.
	embeddedPath := selection.Index()[:len(selection.Index())-1]
	if len(embeddedPath) == 0 {
		return dereferenced(selector.X, receiverType), true
	}
	fields := []*types.Var{}
	current := receiverType
	for _, index := range embeddedPath {
		// A selector looks through a pointer to a struct at every step of the way, as the path does.
		if pointer, isPointer := current.Underlying().(*types.Pointer); isPointer {
			current = pointer.Elem()
		}
		structure, isStructure := current.Underlying().(*types.Struct)
		if !isStructure {
			// Invariant: the type checker records a path through embedded fields of structs alone.
			panic("time_until: a promoted method's path runs through a type that is not a struct")
		}
		fields = append(fields, structure.Field(index))
		current = structure.Field(index).Type()
	}
	embedded := fields[len(fields)-1]
	// The field's name alone selects the shallowest field of that name the package can see, which is the embedded
	// time.Time unless a field nearer the receiver takes its name.
	found, _, _ := types.LookupFieldOrMethod(receiverType, true, context.Package, embedded.Name())
	if found == embedded {
		return dereferenced(&ast.SelectorExpr{X: selector.X, Sel: &ast.Ident{Name: embedded.Name()}}, current), true
	}
	// Every field on the way, by its name, is a field of the struct before it, the nearest of that name there, which
	// the package can name where the field is exported or the package's own.
	reached := selector.X
	for _, field := range fields {
		if !field.Exported() && field.Pkg() != context.Package {
			return nil, false
		}
		reached = &ast.SelectorExpr{X: reached, Sel: &ast.Ident{Name: field.Name()}}
	}
	return dereferenced(reached, current), true
}

// dereferenced is expression where its type is a time.Time, and the time.Time it points at where its type is a pointer
// to one.
func dereferenced(expression ast.Expr, expressionType types.Type) ast.Expr {
	if _, isPointer := expressionType.Underlying().(*types.Pointer); isPointer {
		return &ast.StarExpr{X: expression}
	}
	return expression
}

// importsTime is whether object is the name of an import of the standard library's package time.
func importsTime(object types.Object) bool {
	packageName, isPackageName := object.(*types.PkgName)
	return isPackageName && packageName.Imported().Path() == "time"
}

// isFunction is whether function names the function or method of fullName, as the pattern's `(Symbol fullName)`
// matches it: an identifier or a selector, parenthesized or not, whose object is a function whose types.Func.FullName
// is fullName, so a dot import and a renamed import name time.Now, a promoted Sub and one through a pointer name
// (time.Time).Sub, and a method of another type, an interface's, a variable holding a function and a struct's field
// do not. Symbol also looks through an instantiation, which neither name can have, since time.Now has no type
// parameters and a method declares none of its own. Symbol also names a package's variable, constant or type by its
// package's path and name, and none is spelled either way, since only the standard library's time has the path time,
// and it declares Now as a function and Sub as a method alone.
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
