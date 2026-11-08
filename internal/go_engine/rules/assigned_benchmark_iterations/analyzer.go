// Package assigned_benchmark_iterations is staticcheck's SA3001, an assignment to b.N in a benchmark, which the
// testing package sets and reads to time one operation, ported as a check on syntax that reports through the kit
// every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa3001/sa3001.go, whose logic it keeps and
// whose message it reports word for word, over the same range, the whole assignment. The original matches the
// pattern `(AssignStmt sel@(SelectorExpr selX (Ident "N")) "=" [_])` of staticcheck's pattern package; the port
// tests the same shape by its syntax: a plain assignment, not an operator one or a definition, of one value to one
// selector named N, parentheses around the selector or its operand looked through as the pattern does, whose operand
// is a pointer to testing.B, as code.IsOfPointerToTypeWithName judges through typeutil.IsPointerToTypeWithName,
// which the port calls itself. The message renders the selector with staticcheck's report.Render, called with a pass
// that holds only the file set, which is all it reads; wave 5 ports it. The pattern names no symbol, so the original
// walks every package, and so does the port. It reports through staticcheck_report.Report with no option, so a
// generated file is judged like any other. The registry still runs the original, which stays the oracle: the parity
// tests beside this file run both over the same packages, and the two differ nowhere they hold.
package assigned_benchmark_iterations

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/report"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every assignment, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.AssignStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		assignment, isAssignment := node.(*ast.AssignStmt)
		if !isAssignment {
			// Invariant: the dispatcher hands Node only the assignments it was asked for.
			panic("assigned_benchmark_iterations: the dispatcher handed over a node that is not an assignment")
		}
		Check(context, assignment)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA3001",
	"SA3001",
	"Flags an assignment to b.N in a benchmark, which distorts its results: staticcheck's SA3001, ported as a "+
		"check on syntax.",
	&Listener,
)

// Check judges one assignment: a plain assignment of one value to the field N of a pointer to testing.B.
func Check(context *rule.Context, assignment *ast.AssignStmt) {
	if assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return
	}
	selector, isSelector := ast.Unparen(assignment.Lhs[0]).(*ast.SelectorExpr)
	if !isSelector || selector.Sel.Name != "N" {
		return
	}
	if !typeutil.IsPointerToTypeWithName(context.TypesInfo.TypeOf(ast.Unparen(selector.X)), "testing.B") {
		return
	}
	rendered := report.Render(&analysis.Pass{Fset: context.Fset}, selector)
	staticcheck_report.Report(context, assignment, fmt.Sprintf("should not assign to %s", rendered))
}
