// Package single_argument_append is staticcheck's SA4021, a call of the builtin append with one argument, which
// appends nothing and only hands back its slice, ported as a check on syntax that reports through the kit every port
// of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4021/sa4021.go, whose message it reports
// word for word, over the whole call, through staticcheck_report.Report with FilterGenerated, the option the original
// passes report.Report. The original matches the pattern `(CallExpr (Builtin "append") [_])` through code.Matches; the
// port matches what that pattern matches without importing it: a call expression, any of them, since the pattern names
// no symbol for code.CouldMatchAny to rule a package out by, whose function staticcheck_code.IsBuiltin, the port of the
// pattern's Builtin, takes for the universe's append, and whose arguments are exactly one, of any shape. The pattern
// has no field for a call's ellipsis, so neither does the port. The original reports a call whose one argument is a
// call returning several values, `append(pair())`, which appends the rest, since the pattern counts the syntax's
// arguments; parity's multipleResults shows it, and the port keeps it, as the parity doctrine asks. The original keeps
// nothing from one call to the next, and stays the oracle: the parity tests beside this file run both over the same
// packages, and the two differ nowhere they hold.
package single_argument_append

import (
	"go/ast"

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
			panic("single_argument_append: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4021",
	"SA4021",
	"Flags `x = append(y)`, which is `x = y`: staticcheck's SA4021, ported as a check on syntax.",
	&Listener,
)

// message is the original's, word for word.
const message = "x = append(y) is equivalent to x = y"

// Check judges one call: the builtin append, named through any parentheses and not shadowed, called with one argument.
func Check(context *rule.Context, call *ast.CallExpr) {
	if len(call.Args) != 1 {
		return
	}
	// Through any parentheses, so `(append)(values)` is append too, and never an append the code declares itself.
	if !staticcheck_code.IsBuiltin(context, call.Fun, "append") {
		return
	}
	staticcheck_report.Report(context, call, message, staticcheck_report.FilterGenerated())
}
