// Package empty_critical_section is staticcheck's SA2001, a call to Lock followed at once by a call to Unlock on the
// same receiver, or RLock by RUnlock, which guards nothing, ported as a check on syntax that reports through the kit
// every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa2001/sa2001.go, whose logic it keeps and
// whose message it reports word for word, over the whole unlocking statement, where staticcheck's report puts it. It
// judges two receivers the same when they print the same, through staticcheck_report.Render where the original asks
// report.Render, and reports through staticcheck_report.Report where the original reports through report.Report, each
// the port of the one the original calls, proven against it beside it. Like the original, it judges a call by its
// function's name and empty signature alone, so any type's Lock and Unlock count, not only a mutex's, and so do a
// package's functions called through its name (parity's packageFunctions shows it), and a receiver printed the same
// twice is judged one receiver even when computing it has effects, as `m.x().Lock()` and `m.x().Unlock()` do
// (upstream's fn4), which the original accepts as the price of catching the common case. The registry still runs the
// original, which stays the oracle: the parity tests beside this file run both over the same packages, and the two
// differ nowhere they hold.
package empty_critical_section

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// exemptPackage is the package staticcheck exempts: the sync package's external tests, which lock and unlock at once
// on purpose.
const exemptPackage = "sync_test"

// Listener is what the port hears of each package from the dispatcher: every block, in preorder, but none of the
// exempt package's.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.BlockStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		if context.Package.Path() == exemptPackage {
			return
		}
		block, isBlock := node.(*ast.BlockStmt)
		if !isBlock {
			// Invariant: the dispatcher hands Node only the blocks it was asked for.
			panic("empty_critical_section: the dispatcher handed over a node that is not a block")
		}
		Check(context, block)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA2001",
	"SA2001",
	"Flags a lock released at once, an empty critical section: staticcheck's SA2001, ported as a check on "+
		"syntax.",
	&Listener,
)

// Check judges one block: each statement calling Unlock right after one calling Lock, or RUnlock right after RLock,
// on a receiver that prints the same, is reported.
func Check(context *rule.Context, block *ast.BlockStmt) {
	for index := 0; index+1 < len(block.List); index++ {
		lockReceiver, lockMethod, isLock := methodCall(context, block.List[index])
		unlockReceiver, unlockMethod, isUnlock := methodCall(context, block.List[index+1])
		if !isLock || !isUnlock || staticcheck_report.Render(context, lockReceiver) !=
			staticcheck_report.Render(context, unlockReceiver) {
			continue
		}
		if (lockMethod == "Lock" && unlockMethod == "Unlock") || (lockMethod == "RLock" && unlockMethod == "RUnlock") {
			staticcheck_report.Report(context, block.List[index+1], "empty critical section")
		}
	}
}

// methodCall is the receiver and the method's name of a statement that only calls a method taking and returning
// nothing, such as `mutex.Lock()`, and false for any other statement. Parentheses around the call are looked
// through, and parentheses around the method expression are not, as the original reads them.
func methodCall(context *rule.Context, statement ast.Stmt) (ast.Expr, string, bool) {
	expression, isExpression := statement.(*ast.ExprStmt)
	if !isExpression {
		return nil, "", false
	}
	call, isCall := ast.Unparen(expression.X).(*ast.CallExpr)
	if !isCall {
		return nil, "", false
	}
	selector, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return nil, "", false
	}
	method, isFunction := context.TypesInfo.ObjectOf(selector.Sel).(*types.Func)
	if !isFunction {
		return nil, "", false
	}
	signature, isSignature := method.Type().(*types.Signature)
	if !isSignature || signature.Params().Len() != 0 || signature.Results().Len() != 0 {
		return nil, "", false
	}
	return selector.X, method.Name(), true
}
