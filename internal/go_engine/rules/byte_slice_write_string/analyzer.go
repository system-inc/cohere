// Package byte_slice_write_string is staticcheck's SA6006, a byte slice converted to a string only to be handed to
// io.WriteString, where the writer's Write would take the slice as it is, ported as a check on syntax that reports
// through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa6006/sa6006.go, whose message it reports
// word for word, over the whole call, through staticcheck_report.Report with no option, as the original reports through
// report.Report: a generated file is reported too. The original matches the pattern `(CallExpr (Symbol
// "io.WriteString") [_ (CallExpr (Builtin "string") [arg])])` through code.Matches, which for a pattern rooted at a
// call of a symbol walks the calls staticcheck's type index records of io.WriteString, those whose function, through
// parentheses, a selector and an instantiation, uses its object, rather than every call. The port walks every call and
// asks the same of each: staticcheck_code.IsSymbol, the port of the pattern's Symbol, takes its function for
// io.WriteString, and it has exactly two arguments, the second, through parentheses, a conversion
// staticcheck_code.IsBuiltin, the port of the pattern's Builtin, takes for the universe's string, of exactly one
// argument. Both walks read the object the type checker recorded for the function's identifier, so they reach the same
// findings, in another order. The pattern has no field for a call's ellipsis, so neither does the port. It then asks
// staticcheck_code.IsOfStringConvertibleByteSlice, the port of the helper the original asks, of the converted argument
// with its parentheses removed, as the pattern binds it, and that helper reads the file's language version. The
// original keeps nothing from one call to the next, and stays the oracle: the parity tests beside this file run both
// over the same modules, and the two differ nowhere they hold.
package byte_slice_write_string

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
			panic("byte_slice_write_string: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA6006",
	"SA6006",
	"Flags io.WriteString given a byte slice converted to a string: staticcheck's SA6006, ported as a check "+
		"on syntax.",
	&Listener,
)

// message is the original's, word for word.
const message = "use io.Writer.Write instead of converting from []byte to string to use io.WriteString"

// Check judges one call: io.WriteString given, as its string, a conversion of a slice that converts to a string
// directly.
func Check(context *rule.Context, call *ast.CallExpr) {
	if len(call.Args) != 2 || !staticcheck_code.IsSymbol(context, call.Fun, "io.WriteString") {
		return
	}
	conversion, isCall := ast.Unparen(call.Args[1]).(*ast.CallExpr)
	// A string the code declares itself, a function or a type, is no conversion by the universe's.
	if !isCall || len(conversion.Args) != 1 || !staticcheck_code.IsBuiltin(context, conversion.Fun, "string") {
		return
	}
	if !staticcheck_code.IsOfStringConvertibleByteSlice(context, ast.Unparen(conversion.Args[0])) {
		return
	}
	staticcheck_report.Report(context, call, message)
}
