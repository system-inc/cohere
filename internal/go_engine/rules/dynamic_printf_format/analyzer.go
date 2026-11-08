// Package dynamic_printf_format is staticcheck's SA1006, a printf-style call given a format that is not a literal and
// nothing to format, where the format's own percent signs are read as verbs, ported as a check on syntax that reports
// through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1006/sa1006.go, whose logic it keeps and
// whose message and fix it reports word for word, at the call's Pos and End, through staticcheck_report.Report, the
// port of the report.Report the original calls, proven against it beside it. The original matches two patterns,
// (CallExpr (Symbol name@(Or ...)) format:[]) and (CallExpr (Symbol "fmt.Fprintf") _:format:[]), over the calls
// staticcheck's type index finds of each symbol; the port asks the same of every call by types and syntax, without the
// pattern package: the call's callee, as typeutil.Callee names it, is one of the functions named, declared in a package
// other than the one judged, since the index finds no symbol of the package it indexes; the call has exactly one
// argument, or for fmt.Fprintf exactly two; and the format, its parentheses looked through as the pattern looks
// through them, is an identifier or a call. Its suggested fix is the callee without its final f, or errors.New for
// fmt.Errorf, the callee written as the kit's Render prints it, built with staticcheck's analysis/edit, edit.Fix and
// edit.ReplaceWithString, borrowed in this wave, and passed through staticcheck_report.Fixes; wave 5 ports edit.
//
// It keeps the original's false positives and slips: a constant identifier is a format all the same, a parenthesized
// callee's fix drops the f before the closing parenthesis rather than after it, and fmt.Errorf's fix writes errors.New
// without importing errors; testdata/src/differences holds each, and both sides report them. The original keeps no
// state from one call to the next. The registry still runs the original, which stays the oracle: the parity tests
// beside this file run both over the same packages, fixes included, and the two differ nowhere they hold.
package dynamic_printf_format

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"

	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/go/types/typeutil"

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
			panic("dynamic_printf_format: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1006",
	"SA1006",
	"Flags a printf-style call whose format is not a literal and that has nothing to format, where print-style "+
		"is meant: staticcheck's SA1006, ported as a check on syntax.",
	&Listener,
)

// formatFirstFunctions are the functions whose format is their first argument, by their full names, the original's
// first pattern's.
var formatFirstFunctions = []string{
	"fmt.Errorf",
	"fmt.Printf",
	"fmt.Sprintf",
	"log.Fatalf",
	"log.Panicf",
	"log.Printf",
	"(*log.Logger).Printf",
	"(*testing.common).Logf",
	"(*testing.common).Errorf",
	"(*testing.common).Fatalf",
	"(*testing.common).Skipf",
	"(testing.TB).Logf",
	"(testing.TB).Errorf",
	"(testing.TB).Fatalf",
	"(testing.TB).Skipf",
}

// fprintf is the one function whose format is its second argument, the original's second pattern's.
const fprintf = "fmt.Fprintf"

// errorf is the one function whose print-style form is in another package, errors.New.
const errorf = "fmt.Errorf"

// Check judges one call: a call of a printf-style function given a format that is an identifier or a call and nothing
// else to format is reported, with the fix that calls the print-style function instead.
func Check(context *rule.Context, call *ast.CallExpr) {
	callee, isFunction := typeutil.Callee(context.TypesInfo, call).(*types.Func)
	// staticcheck's type index finds the symbols of the packages a package uses and never its own, so a call of fmt's
	// Printf inside fmt is not one it finds.
	if !isFunction || callee.Pkg() == context.Package {
		return
	}
	name := callee.FullName()
	var format ast.Expr
	switch {
	case slices.Contains(formatFirstFunctions, name) && len(call.Args) == 1:
		format = call.Args[0]
	case name == fprintf && len(call.Args) == 2:
		format = call.Args[1]
	default:
		return
	}
	format = ast.Unparen(format)
	switch format.(type) {
	case *ast.CallExpr, *ast.Ident:
	default:
		return
	}
	// A call returning several values spread into the arguments may well return a format and its arguments.
	if _, isTuple := context.TypesInfo.TypeOf(format).(*types.Tuple); isTuple {
		return
	}
	alternative := "errors.New"
	if name != errorf {
		// A function or a method behind any selector, foo.bar[0].Printf among them: the print-style one is the same
		// callee with the final f taken off.
		rendered := staticcheck_report.Render(context, call.Fun)
		alternative = rendered[:len(rendered)-1]
	}
	staticcheck_report.Report(context, call,
		"printf-style function with dynamic format string and no further arguments should use print-style function "+
			"instead",
		staticcheck_report.Fixes(edit.Fix(fmt.Sprintf("Use %s instead of %s", alternative, name),
			edit.ReplaceWithString(call.Fun, alternative))))
}
