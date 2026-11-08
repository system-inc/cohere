// Package wait_group_add_in_goroutine is staticcheck's SA2000, (*sync.WaitGroup).Add called first thing inside the
// goroutine it counts, where it races the Wait it was meant to come before, ported as a check on syntax that reports
// through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa2000/sa2000.go, whose logic it keeps and
// whose message it reports word for word, over the same range, the call. The original matches the pattern
// `(GoStmt (CallExpr (FuncLit _ call@(CallExpr (Symbol "(*sync.WaitGroup).Add") _):_) _))` of staticcheck's pattern
// package; the port tests the same shape by its syntax: a go statement calling a function literal, parenthesized or
// not, whose first statement is a call to that method. The pattern reads that statement through the layers its match
// looks through, an expression statement, a label, parentheses, and a block of exactly one statement, and so does
// the port (firstCall). The method is judged by staticcheck_code.IsSymbol, the port of the pattern's Symbol, so a call
// through a method expression or a promoted method counts as the pattern counts it. The original first asks whether
// the package depends on sync at all, code.CouldMatchAny, and walks no further when it does not; any call the pattern
// matches resolves into sync, so the question changes no finding, and the port, asking nothing, walks every go
// statement. The message renders the call with staticcheck's report.Render, called with a pass that holds only the
// file set, which is all it reads; wave 5 ports it. It reports through staticcheck_report.Report with no option, so a
// generated file is judged like any other. The registry runs the port in the original's place, under its name, and the
// original stays only as the oracle: the parity tests beside this file run both over the same packages, and the two
// differ nowhere they hold.
package wait_group_add_in_goroutine

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/report"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every go statement, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.GoStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		statement, isGo := node.(*ast.GoStmt)
		if !isGo {
			// Invariant: the dispatcher hands Node only the go statements it was asked for.
			panic("wait_group_add_in_goroutine: the dispatcher handed over a node that is not a go statement")
		}
		Check(context, statement)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA2000",
	"SA2000",
	"Flags (*sync.WaitGroup).Add called first inside the goroutine it counts, which races Wait: staticcheck's "+
		"SA2000, ported as a check on syntax.",
	&Listener,
)

// Check judges one go statement: one that starts a function literal whose first statement adds to a WaitGroup.
func Check(context *rule.Context, statement *ast.GoStmt) {
	literal, isLiteral := ast.Unparen(statement.Call.Fun).(*ast.FuncLit)
	if !isLiteral || len(literal.Body.List) == 0 {
		return
	}
	call := firstCall(literal.Body.List[0])
	if call == nil || !staticcheck_code.IsSymbol(context, call.Fun, "(*sync.WaitGroup).Add") {
		return
	}
	rendered := report.Render(&analysis.Pass{Fset: context.Fset}, call)
	staticcheck_report.Report(context, call,
		fmt.Sprintf("should call %s before starting the goroutine to avoid a race", rendered))
}

// firstCall is the call statement is, read through every layer the pattern's match looks through, or nil when it is
// none: an expression statement, a labeled statement, parentheses, and a block holding exactly one statement, which
// the match compares with the call as a list of one.
func firstCall(statement ast.Node) *ast.CallExpr {
	reached := statement
	for {
		switch layer := reached.(type) {
		case *ast.CallExpr:
			return layer
		case *ast.ExprStmt:
			reached = layer.X
		case *ast.ParenExpr:
			reached = layer.X
		case *ast.LabeledStmt:
			reached = layer.Stmt
		case *ast.BlockStmt:
			if len(layer.List) != 1 {
				return nil
			}
			reached = layer.List[0]
		default:
			return nil
		}
	}
}
