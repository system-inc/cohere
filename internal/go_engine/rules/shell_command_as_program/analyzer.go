// Package shell_command_as_program is staticcheck's SA1005, exec.Command given as its program a constant string that
// reads as a whole shell command, a name and its arguments split by spaces, which exec.Command runs as one program's
// name, ported as a check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1005/sa1005.go, whose logic it keeps and
// whose message it reports word for word, at the same range, the program argument. The original matches its query,
// `(CallExpr (Symbol "os/exec.Command") arg1:_)`, through staticcheck's pattern language; the port asks the same of
// each call directly, reading pattern/match.go for what each construct of the query matches, asks
// staticcheck_code.ExprToString where the original asks code.ExprToString, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere they hold.
package shell_command_as_program

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/function_name"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("shell_command_as_program: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1005",
	"SA1005",
	"Flags exec.Command given as its program a constant string holding a space, a whole shell command where a "+
		"program's name or path is expected: staticcheck's SA1005, ported.",
	&Listener,
)

// Check judges one call: a call of exec.Command whose program, its first argument, is a constant string holding a
// space reads as a shell command, unless it also holds a slash or a backslash, the mark of a path, whose folders may
// hold spaces.
func Check(context *rule.Context, call *ast.CallExpr) {
	if !isExecCommand(context, call.Fun) {
		return
	}
	// `arg1:_` is a list of one or more arguments whose first is arg1. exec.Command's program is a parameter of its
	// own, so a call that type-checks always has it.
	if len(call.Args) == 0 {
		return
	}
	// The matcher reaches the program through its outer parentheses, so the binding, the constant asked of it and the
	// finding's range are the expression inside them.
	program := ast.Unparen(call.Args[0])
	value, isString := staticcheck_code.ExprToString(context, program)
	if !isString {
		return
	}
	if !strings.Contains(value, " ") || strings.Contains(value, `\`) || strings.Contains(value, "/") {
		return
	}
	staticcheck_report.Report(context, program,
		"first argument to exec.Command looks like a shell command, but a program name or path are expected")
}

// isExecCommand is whether function names exec.Command, as the query's `(Symbol "os/exec.Command")` matches it: an
// identifier or a selector, parenthesized or not and instantiated or not, whose object is the function Command of
// the package os/exec, so a dot import and a renamed import name it, and a method, a variable or a local function
// named Command does not. Symbol also names a package's variable, constant or type by its package's path and name,
// and none spelled os/exec.Command exists, since os/exec declares Command as a function alone.
func isExecCommand(context *rule.Context, function ast.Expr) bool {
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
	// The full name Matches compares spells a method with its receiver, `(T).Command`, and a function by its package's
	// path.
	command, isFunction := object.(*types.Func)
	return isFunction && function_name.Matches(command, "os/exec.Command")
}
