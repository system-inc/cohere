// Package spinning_empty_loop is staticcheck's SA5002, an empty for loop that spins, keeping a core busy and the
// scheduler blocked, ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5002/sa5002.go, whose logic it keeps and
// whose messages it reports word for word, at the same ranges: the condition's finding spans the whole loop, and the
// spin's spans its header, as staticcheck's report.ShortRange cuts it. It asks staticcheck_code.MayHaveSideEffects
// where the original asks code.MayHaveSideEffects, and reports through staticcheck_report.Report where the original
// reports through report.Report, each the port of the one the original calls, proven against it beside it. The
// registry still runs the original, which stays the oracle: the parity tests beside this file run both over the same
// packages, and the two differ nowhere they hold.
package spinning_empty_loop

import (
	"go/ast"
	"go/constant"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every for statement, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.ForStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		loop, isLoop := node.(*ast.ForStmt)
		if !isLoop {
			// Invariant: the dispatcher hands Node only the for statements it was asked for.
			panic("spinning_empty_loop: the dispatcher handed over a node that is not a for statement")
		}
		Check(context, loop)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA5002",
	"SA5002",
	"Flags an empty for loop, which spins and can block the scheduler: staticcheck's SA5002, ported as a "+
		"check on syntax.",
	&Listener,
)

// Check judges one for loop: one with no body, no post statement and no initializer spins when it has no condition,
// and when its condition can change only through a data race, it also races.
func Check(context *rule.Context, loop *ast.ForStmt) {
	if len(loop.Body.List) != 0 || loop.Post != nil {
		return
	}
	// A loop with an initializer could spin as well; staticcheck leaves it alone only to keep the check simple.
	if loop.Init != nil {
		return
	}
	// A condition that calls a function or receives from a channel changes as the program runs, and the loop may end.
	// One that only reads variables and fields, and operates on what it read, changes only if another goroutine
	// writes them unsynchronized, which is a data race. No function is judged pure here, so every call counts.
	if loop.Cond != nil {
		if staticcheck_code.MayHaveSideEffects(context, loop.Cond, nil) {
			return
		}
		if identifier, isIdentifier := loop.Cond.(*ast.Ident); isIdentifier {
			constantCondition, isConstant := context.TypesInfo.ObjectOf(identifier).(*types.Const)
			// `for false {}` is left alone, a loop switched off for debugging. A `false` the code declares itself is a
			// variable, and is judged like any other.
			if isConstant && !constant.BoolVal(constantCondition.Val()) {
				return
			}
		}
		staticcheck_report.Report(context, loop, "loop condition never changes or has a race condition")
	}
	staticcheck_report.Report(context, loop, "this loop will spin, using 100% CPU", staticcheck_report.ShortRange())
}
