// Package defer_in_channel_range_loop is staticcheck's SA9001, a defer in a loop that ranges over a channel, which
// runs only once the function returns, and so only once the channel is closed, ported as a check on syntax that
// reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa9001/sa9001.go, whose logic it keeps and
// whose message it reports word for word, at each defer's Pos and End, through staticcheck_report.Report, the port of
// the report.Report the original calls, proven against it beside it. A loop over a value of a type parameter whose
// core type is a channel counts, as it does for the original, through typeutil.CoreType.
//
// It keeps the original's judgment of whether the loop might end, quirk and all: a return marks the loop as one that
// might end, and each break or continue after it, wherever it sits outside a closure, marks it again as might end when
// it is a break and as never ends when it is not, so the last branch statement of the walk decides. A continue after
// the loop's only return or break therefore has its defers reported, where the loop can in fact end: a false positive
// the original makes, shown by continueAfterReturn in testdata/src/differences. The port makes it too, so both report
// it and the fixture records no difference there; dropping it waits for its own task once the port has taken the
// original's place, since the parity tests allow a finding the port adds, never one it drops. The original keeps no
// state from one loop to the next. The registry still runs the original, which stays the oracle: the parity tests
// beside this file run both over the same packages, and the two differ nowhere they hold.
package defer_in_channel_range_loop

import (
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every range statement, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.RangeStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		loop, isLoop := node.(*ast.RangeStmt)
		if !isLoop {
			// Invariant: the dispatcher hands Node only the range statements it was asked for.
			panic("defer_in_channel_range_loop: the dispatcher handed over a node that is not a range statement")
		}
		Check(context, loop)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA9001",
	"SA9001",
	"Flags a defer in a loop that ranges over a channel, which runs only once the channel is closed and the "+
		"function returns: staticcheck's SA9001, ported as a check on syntax.",
	&Listener,
)

// Check judges one range loop: one over a channel whose body, closures aside, the walk does not judge might end has
// every defer in its body reported.
func Check(context *rule.Context, loop *ast.RangeStmt) {
	if _, isChannel := typeutil.CoreType(context.TypesInfo.TypeOf(loop.X)).(*types.Chan); !isChannel {
		return
	}
	defers := []*ast.DeferStmt{}
	mightEnd := false
	ast.Inspect(loop.Body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.DeferStmt:
			defers = append(defers, statement)
		case *ast.FuncLit:
			// A function literal's defers and returns are its own, not the loop's.
			return false
		case *ast.ReturnStmt:
			mightEnd = true
		case *ast.BranchStmt:
			// Each branch statement overwrites what came before, as the original's does (see the package comment).
			mightEnd = statement.Tok == token.BREAK
		}
		return true
	})
	if mightEnd {
		return
	}
	for _, deferred := range defers {
		staticcheck_report.Report(context, deferred,
			"defers in this range loop won't run unless the channel gets closed")
	}
}
