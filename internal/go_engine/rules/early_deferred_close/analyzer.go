// Package early_deferred_close is staticcheck's SA5001, a deferred Close on a value right after the call that made
// it, before the call's error is checked, so a failed call's value, often nil, is closed, ported as a check on syntax
// that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5001/sa5001.go, whose logic it keeps and
// whose message it reports word for word, over the whole defer statement, where staticcheck's report puts it. It
// prints the call and the deferred call into the message through staticcheck_report.Render where the original asks
// report.Render, and reports through staticcheck_report.Report where the original reports through report.Report, each
// the port of the one the original calls, proven against it beside it. Like the original, it takes a last result
// whose type prints as `error` for the error, and it judges the closed value by the name its selector starts from, so
// `t.rc.Close()` after `t, err := f()` counts (upstream's fn2 shows both). The registry still runs the original, which
// stays the oracle: the parity tests beside this file run both over the same packages, and the two differ nowhere
// they hold.
package early_deferred_close

import (
	"fmt"
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every block, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.BlockStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		block, isBlock := node.(*ast.BlockStmt)
		if !isBlock {
			// Invariant: the dispatcher hands Node only the blocks it was asked for.
			panic("early_deferred_close: the dispatcher handed over a node that is not a block")
		}
		Check(context, block)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA5001",
	"SA5001",
	"Flags a Close deferred before the error of the call that made the value is checked: staticcheck's "+
		"SA5001, ported as a check on syntax.",
	&Listener,
)

// Check judges one block: each assignment of a call's results, the last of them an error kept in a variable, followed
// at once by a defer of Close on a selector rooted at the first variable assigned, is reported.
func Check(context *rule.Context, block *ast.BlockStmt) {
	for index := 0; index+1 < len(block.List); index++ {
		assignment, isAssignment := block.List[index].(*ast.AssignStmt)
		if !isAssignment || len(assignment.Rhs) != 1 || len(assignment.Lhs) < 2 {
			continue
		}
		// An error assigned to the blank identifier is thrown away, so no check of it can come after the defer.
		if last, isName := assignment.Lhs[len(assignment.Lhs)-1].(*ast.Ident); isName && last.Name == "_" {
			continue
		}
		call, isCall := assignment.Rhs[0].(*ast.CallExpr)
		if !isCall || !returnsErrorLast(context, call) {
			continue
		}
		first, isFirstName := assignment.Lhs[0].(*ast.Ident)
		if !isFirstName {
			continue
		}
		deferred, isDefer := block.List[index+1].(*ast.DeferStmt)
		if !isDefer {
			continue
		}
		selector, isSelector := deferred.Call.Fun.(*ast.SelectorExpr)
		if !isSelector {
			continue
		}
		root, isRootName := selectorRoot(selector).(*ast.Ident)
		if !isRootName || context.TypesInfo.ObjectOf(root) != context.TypesInfo.ObjectOf(first) {
			continue
		}
		if selector.Sel.Name != "Close" {
			continue
		}
		staticcheck_report.Report(context, deferred, fmt.Sprintf(
			"should check error returned from %s() before deferring %s",
			staticcheck_report.Render(context, call.Fun), staticcheck_report.Render(context, deferred.Call)))
	}
}

// returnsErrorLast is whether call is of a function with two results or more, the last of a type that prints as
// `error`. The original compares the printed type, so any type that prints so counts, and a defined type of another
// package named error does not, since it prints with its package's path.
func returnsErrorLast(context *rule.Context, call *ast.CallExpr) bool {
	signature, isSignature := context.TypesInfo.TypeOf(call.Fun).(*types.Signature)
	if !isSignature || signature.Results().Len() < 2 {
		return false
	}
	return signature.Results().At(signature.Results().Len()-1).Type().String() == "error"
}

// selectorRoot is the expression a chain of selectors starts from, `t` of `t.rc.Close`. Only selectors are walked
// through, so `(t).rc` or `t[0].rc` roots at the parentheses or the index, which is no name.
func selectorRoot(selector *ast.SelectorExpr) ast.Node {
	if inner, isSelector := selector.X.(*ast.SelectorExpr); isSelector {
		return selectorRoot(inner)
	}
	return selector.X
}
