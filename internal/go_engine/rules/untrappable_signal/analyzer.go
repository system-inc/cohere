// Package untrappable_signal is staticcheck's SA1016, `os.Kill`, `syscall.SIGKILL` or `syscall.SIGSTOP` handed to
// signal.Notify, Ignore or Reset, signals no program can catch, ported as a check on syntax that reports through the
// kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1016/sa1016.go, whose logic it keeps and
// whose messages and fixes it reports word for word, over the same range, the signal argument, inside its conversion
// to os.Signal when it is converted. The original finds the calls with the pattern `(CallExpr (Symbol (Or
// "os/signal.Ignore" "os/signal.Notify" "os/signal.Reset")) _)` of staticcheck's pattern package, which, rooted at a
// call of named symbols, walks the type index's calls of those functions rather than every node; the port walks
// every call and asks staticcheck_code.SymbolIndex of its function, which accepts exactly the calls the index lists, a
// function named directly, through parentheses, a selector or a dot import, and refuses one held in a variable,
// which the index does not list either. The original first asks whether the package depends on os/signal at all,
// code.CouldMatchAny; any call it would judge calls into it, so the question changes no finding, and the port asks
// nothing. Each argument is named by staticcheck_code.SelectorName as the original names it with code.SelectorName,
// and only when it is a selector, unparenthesized, as the original asks. The fixes build each call anew from a copy
// of the original's syntax, as the original does, and print it with staticcheck's analysis/edit, borrowed in this wave
// as the original uses it, through staticcheck_report.Fixes; messages render code with staticcheck_report.Render. It
// reports with no option, so a generated file is judged like any other. The registry runs the port in the original's
// place, under its name, and the original stays only as the oracle: the parity tests beside this file run both over the
// same packages, and the two differ nowhere they hold.
package untrappable_signal

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// signalFunctions are the functions of os/signal that take signals as their arguments, as the pattern names them.
var signalFunctions = []string{"os/signal.Ignore", "os/signal.Notify", "os/signal.Reset"}

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("untrappable_signal: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1016",
	"SA1016",
	"Flags os.Kill, syscall.SIGKILL or syscall.SIGSTOP handed to signal.Notify, Ignore or Reset, which cannot "+
		"be trapped: staticcheck's SA1016, ported as a check on syntax.",
	&Listener,
)

// Check judges one call: each signal argument of a call to signal.Notify, Ignore or Reset that no program can trap.
func Check(context *rule.Context, call *ast.CallExpr) {
	if !callsSignalFunction(context, call) {
		return
	}
	hasTerminate := false
	for _, argument := range call.Args {
		if isSignal(context, signalOf(context, argument), "syscall.SIGTERM") {
			hasTerminate = true
			break
		}
	}
	for index, argument := range call.Args {
		signal := signalOf(context, argument)
		if isSignal(context, signal, "os.Kill") || isSignal(context, signal, "syscall.SIGKILL") {
			rendered := staticcheck_report.Render(context, signal)
			fixes := []analysis.SuggestedFix{}
			// Suggesting `syscall.SIGTERM` in its place would name it twice when the call already traps it.
			if !hasTerminate {
				replaced := *call
				replaced.Args = replacedArgument(call.Args, index, edit.Selector("syscall", "SIGTERM"))
				fixes = append(fixes, edit.Fix(fmt.Sprintf("Use syscall.SIGTERM instead of %s", rendered),
					edit.ReplaceWithNode(context.Fset, call, &replaced)))
			}
			removed := *call
			removed.Args = removedArgument(call.Args, index)
			fixes = append(fixes, edit.Fix(fmt.Sprintf("Remove %s from list of arguments", rendered),
				edit.ReplaceWithNode(context.Fset, call, &removed)))
			staticcheck_report.Report(context, signal,
				fmt.Sprintf("%s cannot be trapped (did you mean syscall.SIGTERM?)", rendered),
				staticcheck_report.Fixes(fixes...))
		}
		if isSignal(context, signal, "syscall.SIGSTOP") {
			removed := *call
			removed.Args = removedArgument(call.Args, index)
			staticcheck_report.Report(context, signal, "syscall.SIGSTOP cannot be trapped",
				staticcheck_report.Fixes(edit.Fix("Remove syscall.SIGSTOP from list of arguments",
					edit.ReplaceWithNode(context.Fset, call, &removed))))
		}
	}
}

// callsSignalFunction is whether call calls one of signalFunctions, as the pattern's Symbol of their Or judges it,
// which staticcheck_code.SymbolIndex answers resolving the call's function once for all three names.
func callsSignalFunction(context *rule.Context, call *ast.CallExpr) bool {
	return staticcheck_code.SymbolIndex(context, call.Fun, signalFunctions...) >= 0
}

// signalOf is the signal argument names: the operand of its conversion to os.Signal when it is one, and argument
// itself otherwise.
func signalOf(context *rule.Context, argument ast.Expr) ast.Expr {
	conversion, isCall := argument.(*ast.CallExpr)
	// A conversion that type-checks has exactly one operand, which the original reads without asking.
	if isCall && len(conversion.Args) > 0 && isSignal(context, conversion.Fun, "os.Signal") {
		return conversion.Args[0]
	}
	return argument
}

// isSignal is whether expression is a selector, unparenthesized, of what staticcheck names name, "os.Kill" or
// "syscall.SIGTERM": the original asks nothing else of it.
func isSignal(context *rule.Context, expression ast.Expr, name string) bool {
	selector, isSelector := expression.(*ast.SelectorExpr)
	return isSelector && staticcheck_code.SelectorName(context, selector) == name
}

// replacedArgument is arguments with the one at index replaced by replacement, in a slice of its own.
func replacedArgument(arguments []ast.Expr, index int, replacement ast.Expr) []ast.Expr {
	replaced := make([]ast.Expr, len(arguments))
	for each, argument := range arguments {
		if each == index {
			replaced[each] = replacement
			continue
		}
		replaced[each] = argument
	}
	return replaced
}

// removedArgument is arguments without the one at index, in a slice of its own.
func removedArgument(arguments []ast.Expr, index int) []ast.Expr {
	removed := make([]ast.Expr, 0, len(arguments))
	for each, argument := range arguments {
		if each != index {
			removed = append(removed, argument)
		}
	}
	return removed
}
