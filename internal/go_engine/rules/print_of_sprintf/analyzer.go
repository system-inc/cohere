// Package print_of_sprintf is staticcheck's S1038, a print of fmt.Sprintf's result where the print's own formatting
// form would do, fmt.Print(fmt.Sprintf(...)) for fmt.Printf(...), ported as a check on types and syntax that reports
// through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1038/s1038.go, whose three forms it keeps and
// whose messages it reports word for word, over the same range, the whole outer call. The original runs three pattern
// queries on every call of every package that could hold one, and each query allocates its matcher's state before it
// can fail; the port fails first on a gate that allocates nothing, the call's last argument resolving by types to
// fmt.Sprintf, and only past it asks what the three forms ask. It asks staticcheck_code.CallName where the original
// asks code.CallName, and reports through staticcheck_report.Report where the original reports through
// report.Report, each the port of the one the original calls. The registry still runs the original, which stays the
// oracle: the parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package print_of_sprintf

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	// The original first asks staticcheck's type index whether the package could hold any of its queries' symbols at
	// all. The port needs no such filter: the gate in Check fails as cheaply on every call of a package that never
	// names fmt.Sprintf, and every form wants it. Where the filter answers otherwise than the gate, inside fmt
	// itself, Check says why.
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("print_of_sprintf: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1038",
	"S1038",
	"Flags a print of fmt.Sprintf's result where the print's formatting form would do, staticcheck's S1038, "+
		"ported.",
	&Listener,
)

// alternative is the formatting form a print's finding names: the receiver its method must be declared on, as
// typeutil.FuncName writes one and empty for a package's function, and the form's name, a method's own or a
// package's function qualified.
type alternative struct {
	receiver string
	name     string
}

// alternatives maps each print the method and log forms judge, by its typeutil.FuncName, to its formatting form.
var alternatives = map[string]alternative{
	"(*testing.common).Error": {"(*testing.common)", "Errorf"},
	"(testing.TB).Error":      {"(testing.TB)", "Errorf"},
	"(*testing.common).Fatal": {"(*testing.common)", "Fatalf"},
	"(testing.TB).Fatal":      {"(testing.TB)", "Fatalf"},
	"(*testing.common).Log":   {"(*testing.common)", "Logf"},
	"(testing.TB).Log":        {"(testing.TB)", "Logf"},
	"(*testing.common).Skip":  {"(*testing.common)", "Skipf"},
	"(testing.TB).Skip":       {"(testing.TB)", "Skipf"},
	"(*log.Logger).Fatal":     {"(*log.Logger)", "Fatalf"},
	"(*log.Logger).Fatalln":   {"(*log.Logger)", "Fatalf"},
	"(*log.Logger).Panic":     {"(*log.Logger)", "Panicf"},
	"(*log.Logger).Panicln":   {"(*log.Logger)", "Panicf"},
	"(*log.Logger).Print":     {"(*log.Logger)", "Printf"},
	"(*log.Logger).Println":   {"(*log.Logger)", "Printf"},
	"log.Fatal":               {"", "log.Fatalf"},
	"log.Fatalln":             {"", "log.Fatalf"},
	"log.Panic":               {"", "log.Panicf"},
	"log.Panicln":             {"", "log.Panicf"},
	"log.Print":               {"", "log.Printf"},
	"log.Println":             {"", "log.Printf"},
}

// Check judges one call: a print given the result of fmt.Sprintf as its one argument, or as its second after a
// writer, is reported with the formatting form that would do the work itself.
func Check(context *rule.Context, call *ast.CallExpr) {
	// The gate. Every form wants a call of one or two arguments whose last is a call of fmt.Sprintf, and almost no
	// call of a package is one, so this is asked first and allocates nothing: a length, two type assertions, a map
	// lookup and string comparisons. The original allocates a matcher's state for each of its three queries before
	// any of them can fail, on every call, which made S1038 the costliest of staticcheck's checks on cohere's tree.
	if len(call.Args) != 1 && len(call.Args) != 2 {
		return
	}
	// Every pattern the original matches strips parentheses at each node, so `fmt.Print((fmt.Sprintf(...)))` and
	// `(fmt.Sprintf)(...)` are its findings as well.
	formatted, isCall := ast.Unparen(call.Args[len(call.Args)-1]).(*ast.CallExpr)
	if !isCall {
		return
	}
	sprintf, isFunction := packageFunction(context, formatted.Fun, "fmt")
	if !isFunction || sprintf.Name() != "Sprintf" {
		return
	}
	// The original runs no form at all in a package its type index rules out, and the index holds the packages a
	// package imports, never the package itself. Each of its three queries wants fmt.Sprintf, the method query
	// nothing more, so the index rules out fmt alone: every other package that names fmt.Sprintf imports fmt. Inside
	// log, which imports fmt, the method query passes, and all three forms run, the log form among them.
	if sprintf.Pkg() == context.Package {
		return
	}
	checkFormatPrint(context, call, formatted)
	if len(call.Args) != 1 {
		// The method and log forms take the formatted string as their only argument.
		return
	}
	checkMethodPrint(context, call)
	checkLogPrint(context, call)
}

// checkFormatPrint reports fmt.Print, Sprint and Println given fmt.Sprintf's result, and fmt.Fprint and Fprintln
// given it after their writer. A Println form is reported only when the format is a literal, since one passed in may
// not end in the newline the formatting form would then have to write.
func checkFormatPrint(context *rule.Context, call *ast.CallExpr, formatted *ast.CallExpr) {
	printFunction, isFunction := packageFunction(context, call.Fun, "fmt")
	if !isFunction {
		return
	}
	switch printFunction.Name() {
	case "Print", "Sprint", "Println", "Sprintln":
		if len(call.Args) != 1 {
			return
		}
	case "Fprint", "Fprintln":
		if len(call.Args) != 2 {
			return
		}
	default:
		return
	}
	// The original's query binds Sprintf's first argument, so it wants one, as a Sprintf that type-checks has.
	if len(formatted.Args) == 0 {
		return
	}
	var message string
	switch printFunction.Name() {
	case "Print", "Fprint", "Sprint":
		message = fmt.Sprintf("should use fmt.%s instead of fmt.%s(fmt.Sprintf(...))", printFunction.Name()+"f",
			printFunction.Name())
	default:
		// A format in parentheses is bound without them, as the original's query binds every node.
		if _, isLiteral := ast.Unparen(formatted.Args[0]).(*ast.BasicLit); !isLiteral {
			return
		}
		message = fmt.Sprintf(
			"should use fmt.%s instead of fmt.%s(fmt.Sprintf(...)) (but don't forget the newline)",
			printFunction.Name()[:len(printFunction.Name())-len("ln")]+"f", printFunction.Name(),
		)
	}
	staticcheck_report.Report(context, call, message, staticcheck_report.FilterGenerated())
}

// checkMethodPrint reports a method of testing's or log's that prints given fmt.Sprintf's result, when the formatting
// method the receiver would call instead is the one declared beside it, and not one a type embedding it declares.
func checkMethodPrint(context *rule.Context, call *ast.CallExpr) {
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return
	}
	switch selector.Sel.Name {
	case "Error", "Fatal", "Fatalln", "Log", "Panic", "Panicln", "Print", "Println", "Skip":
	default:
		return
	}
	mapped, isMapped := alternatives[staticcheck_code.CallName(context, call)]
	if !isMapped {
		return
	}
	// The original binds the receiver without its parentheses, so `(t).Error(...)` looks Errorf up on t's type, and
	// its message names t.Errorf beside (t).Error.
	receiver := ast.Unparen(selector.X)
	// A package's name has no type, so log.Print, which the log form judges, is no method here.
	receiverType, isTyped := context.TypesInfo.Types[receiver]
	if !isTyped {
		return
	}
	object, _, _ := types.LookupFieldOrMethod(receiverType.Type, receiverType.Addressable(), nil, mapped.name)
	method, isMethod := object.(*types.Func)
	if !isMethod {
		return
	}
	if typeutil.FuncName(method) != mapped.receiver+"."+mapped.name {
		return
	}
	replacement := &ast.SelectorExpr{X: receiver, Sel: &ast.Ident{Name: mapped.name}}
	staticcheck_report.Report(context, call, fmt.Sprintf("should use %s(...) instead of %s(fmt.Sprintf(...))",
		render(context, replacement), render(context, selector)))
}

// checkLogPrint reports one of the log package's printing functions given fmt.Sprintf's result.
func checkLogPrint(context *rule.Context, call *ast.CallExpr) {
	printFunction, isFunction := packageFunction(context, call.Fun, "log")
	if !isFunction {
		return
	}
	switch printFunction.Name() {
	case "Fatal", "Fatalln", "Panic", "Panicln", "Print", "Println":
	default:
		return
	}
	callName := staticcheck_code.CallName(context, call)
	mapped, isMapped := alternatives[callName]
	if !isMapped {
		return
	}
	staticcheck_report.Report(context, call,
		fmt.Sprintf("should use %s(...) instead of %s(fmt.Sprintf(...))", mapped.name, callName))
}

// packageFunction is the function of the package at path that callee names, and false for anything else. It resolves
// a callee as the original's Symbol query does: without parentheses, without one instantiation's type arguments, and
// then an identifier, a dot import's among them, or a selector's selected name, through the identifiers' uses, since
// a callee is never a definition. Symbol also names variables, constants and types, but none of the package's that
// the forms ask for is one, so a function is the only answer that could match.
func packageFunction(context *rule.Context, callee ast.Expr, path string) (*types.Func, bool) {
	named := ast.Unparen(callee)
	switch instantiated := named.(type) {
	case *ast.IndexExpr:
		named = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		named = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch named := named.(type) {
	case *ast.Ident:
		object = context.TypesInfo.Uses[named]
	case *ast.SelectorExpr:
		object = context.TypesInfo.Uses[named.Sel]
	default:
		return nil, false
	}
	function, isFunction := object.(*types.Func)
	if !isFunction || function.Pkg() == nil || function.Pkg().Path() != path {
		return nil, false
	}
	// A method's full name is its receiver's and its own, never its package's path and its own.
	if function.Signature().Recv() != nil {
		return nil, false
	}
	return function, true
}

// render is node as the original's messages write it, printed by go/format over the package's positions as
// staticcheck's report.Render prints it.
func render(context *rule.Context, node ast.Node) string {
	var buffer bytes.Buffer
	if formatError := format.Node(&buffer, context.Fset, node); formatError != nil {
		// Invariant: the nodes rendered are a selector of the package's own syntax and one built from it, which
		// go/format always prints; staticcheck's report.Render panics here too.
		panic(formatError)
	}
	return buffer.String()
}
