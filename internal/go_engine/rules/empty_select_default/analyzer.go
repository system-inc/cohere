// Package empty_select_default is staticcheck's SA5004, an empty default case in a select that is all an endless for
// loop holds, which makes the loop spin, ported as a check on syntax that reports through the kit every port of
// staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5004/sa5004.go, whose logic it keeps and
// whose message and fix it reports word for word, over the same range, the default case. The original matches the
// pattern `(ForStmt nil nil nil (SelectStmt body))` of staticcheck's pattern package; the port tests the same shape
// by its syntax: a for statement with no initializer, condition or post statement whose body is one statement, a
// select, read through the labels and one-statement blocks the pattern's match looks through (selectOf). The fix
// deletes the case with staticcheck's analysis/edit, borrowed in this wave as the original uses it, through
// staticcheck_report.Fixes; like the original's, it leaves behind an empty line, and any comment in the case. The
// pattern names no symbol, so the original walks every package, and so does the port. It reports with no option, so a
// generated file is judged like any other. The registry runs the port in the original's place, under its name, and the
// original stays only as the oracle: the parity tests beside this file run both over the same packages, and the two
// differ nowhere they hold.
package empty_select_default

import (
	"go/ast"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every for statement, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.ForStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		loop, isLoop := node.(*ast.ForStmt)
		if !isLoop {
			// Invariant: the dispatcher hands Node only the for statements it was asked for.
			panic("empty_select_default: the dispatcher handed over a node that is not a for statement")
		}
		Check(context, loop)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA5004",
	"SA5004",
	"Flags an empty default case in a select that is all an endless for loop holds, which spins: staticcheck's "+
		"SA5004, ported as a check on syntax.",
	&Listener,
)

// Check judges one for loop: an endless one whose body is a select, reported at that select's default case when the
// case is empty.
func Check(context *rule.Context, loop *ast.ForStmt) {
	if loop.Init != nil || loop.Cond != nil || loop.Post != nil {
		return
	}
	selected := selectOf(loop.Body)
	if selected == nil {
		return
	}
	for _, statement := range selected.Body.List {
		clause, isClause := statement.(*ast.CommClause)
		if !isClause || clause.Comm != nil || len(clause.Body) != 0 {
			continue
		}
		staticcheck_report.Report(context, clause,
			"should not have an empty default case in a for+select loop; the loop will spin",
			staticcheck_report.Fixes(edit.Fix("Remove empty default branch", edit.Delete(clause))))
		// A select has one default case at most.
		break
	}
}

// selectOf is the select statement is, read through every layer the pattern's match looks through, or nil when it is
// none: a label, and a block holding exactly one statement, which the match compares with the select as a list of
// one. The loop's own body is such a block.
func selectOf(statement ast.Stmt) *ast.SelectStmt {
	reached := statement
	for {
		switch layer := reached.(type) {
		case *ast.SelectStmt:
			return layer
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
