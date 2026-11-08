// Package infinite_loop_condition is staticcheck's S1006, a for loop whose only clause is a condition that is the
// constant true, `for true {}`, which `for {}` says plainer, ported as a check on types and syntax that reports through
// the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1006/s1006.go, whose logic it keeps and whose
// message it reports word for word, at the same range, the loop's header. The original matches no query through
// staticcheck's pattern language: it walks every for statement and asks code.IsBoolConst and code.BoolConst of its
// condition, which the port asks of the same statements through the kit's staticcheck_code, and it reports through
// staticcheck_report.Report where the original reports through report.Report. The registry still runs the original,
// which stays the oracle: the parity tests beside this file run both over the same packages, and the two differ
// nowhere they hold.
package infinite_loop_condition

import (
	"go/ast"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every for statement, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.ForStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		loop, isFor := node.(*ast.ForStmt)
		if !isFor {
			// Invariant: the dispatcher hands Node only the for statements it was asked for.
			panic("infinite_loop_condition: the dispatcher handed over a node that is not a for statement")
		}
		Check(context, loop)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1006",
	"S1006",
	"Flags a for loop whose sole clause is the constant true: staticcheck's S1006, ported.",
	&Listener,
)

// Check judges one for statement: with no initializer and no post statement, a condition that is an identifier naming
// a boolean constant whose value is true says what a loop with no condition already says, and is reported at the
// loop's header.
func Check(context *rule.Context, loop *ast.ForStmt) {
	// A loop with an initializer or a post statement can't drop its condition alone, since `for i := 0; ; i++ {}` is
	// no plainer, so the original leaves it whatever its condition.
	if loop.Init != nil || loop.Post != nil {
		return
	}
	// IsBoolConst holds only of an identifier, so a missing condition, a parenthesized `(true)`, `!false` and
	// `1 == 1` are none, and neither is a constant of a named or aliased boolean type. It is asked first, as the
	// original asks it, since BoolConst panics on anything it refuses.
	if !staticcheck_code.IsBoolConst(context, loop.Cond) || !staticcheck_code.BoolConst(context, loop.Cond) {
		return
	}
	staticcheck_report.Report(context, loop, "should use for {} instead of for true {}",
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated())
}
