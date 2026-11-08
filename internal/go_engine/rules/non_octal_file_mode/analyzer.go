// Package non_octal_file_mode is staticcheck's SA9002, a file mode written as a decimal literal of three octal digits,
// 644 where 0644 was meant, ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa9002/sa9002.go, whose logic it keeps and
// whose message it reports word for word, at the literal's Pos and End, through staticcheck_report.Report, the port of
// the report.Report the original calls, proven against it beside it. Its suggested fix, the literal with a leading 0,
// is built with staticcheck's analysis/edit, edit.Fix and edit.ReplaceWithString, borrowed in this wave as
// report.Render is, and passed through staticcheck_report.Fixes; wave 5 ports edit. A literal counts only as a call's
// argument itself, not through parentheses or inside an expression, and only when the type checker gave it the type
// os.FileMode or io/fs.FileMode, read with typeutil.IsTypeWithName. The original keeps no state from one call to the
// next. The registry still runs the original, which stays the oracle: the parity tests beside this file run both over
// the same packages, fixes included, and the two differ nowhere they hold.
package non_octal_file_mode

import (
	"fmt"
	"go/ast"
	"strconv"

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
			panic("non_octal_file_mode: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA9002",
	"SA9002",
	"Flags a file mode written as a decimal literal of three octal digits, 644 where 0644 was meant: "+
		"staticcheck's SA9002, ported as a check on syntax.",
	&Listener,
)

// Check judges one call: each argument that is a literal file mode of three digits, the first from 1 to 7 and the
// others from 0 to 7, is reported, with the fix that makes it octal.
func Check(context *rule.Context, call *ast.CallExpr) {
	for _, argument := range call.Args {
		literal, isLiteral := argument.(*ast.BasicLit)
		if !isLiteral {
			continue
		}
		literalType := context.TypesInfo.TypeOf(literal)
		if !typeutil.IsTypeWithName(literalType, "os.FileMode") &&
			!typeutil.IsTypeWithName(literalType, "io/fs.FileMode") {
			continue
		}
		if !looksOctal(literal.Value) {
			continue
		}
		// Three digits always parse; the original skips a literal that does not all the same.
		value, parseError := strconv.ParseInt(literal.Value, 10, 64)
		if parseError != nil {
			continue
		}
		staticcheck_report.Report(context, argument,
			fmt.Sprintf("file mode '%s' evaluates to %#o; did you mean '0%s'?", literal.Value, value, literal.Value),
			staticcheck_report.Fixes(edit.Fix("Fix octal literal",
				edit.ReplaceWithString(argument, "0"+literal.Value))))
	}
}

// looksOctal is whether value, a literal as written, is three digits that read as an octal mode once a 0 is put
// before them: the first from 1 to 7, the others from 0 to 7.
func looksOctal(value string) bool {
	return len(value) == 3 &&
		value[0] != '0' && isOctalDigit(value[0]) &&
		isOctalDigit(value[1]) &&
		isOctalDigit(value[2])
}

// isOctalDigit is whether character is a digit from 0 to 7.
func isOctalDigit(character byte) bool {
	return character >= '0' && character <= '7'
}
