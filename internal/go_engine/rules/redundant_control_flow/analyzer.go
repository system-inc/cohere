// Package redundant_control_flow is staticcheck's S1023, control flow that says what already happens: a `break` that
// ends a case clause, which never falls through, and a `return` that ends a function with no results, which returns
// there anyway. It is ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1023/s1023.go, whose logic it keeps and whose
// messages it reports word for word, at the same ranges, the break or the return. The original matches no pattern; it
// walks the case clauses in one preorder and then the function declarations and literals in a second, and the port
// walks all three kinds in one, which reports the same findings in another order, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere they hold.
package redundant_control_flow

import (
	"go/ast"
	"go/token"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every case clause, function declaration and
// function literal, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CaseClause)(nil), (*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)},
	Node:  func(context *rule.Context, node ast.Node, _ []ast.Node) { Check(context, node) },
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1023",
	"S1023",
	"Flags a break that ends a case clause and a return that ends a function with no results: "+
		"staticcheck's S1023, ported.",
	&Listener,
)

// Check judges one node: a case clause by its last statement, and a function declaration or literal by its results
// and its body's last statement.
func Check(context *rule.Context, node ast.Node) {
	switch node := node.(type) {
	case *ast.CaseClause:
		checkCaseClause(context, node)
	case *ast.FuncDecl:
		checkFunction(context, node.Type, node.Body)
	case *ast.FuncLit:
		checkFunction(context, node.Type, node.Body)
	default:
		// Invariant: Preorder hands this function only the case clauses and functions it was asked for.
		panic("redundant_control_flow: the inspector handed over a node that is neither a case clause nor a function")
	}
}

// checkCaseClause reports the break that ends clause, an expression switch's or a type switch's, after at least one
// other statement. A select's clause is a CommClause, which the walk never asks for, so its break is left alone.
func checkCaseClause(context *rule.Context, clause *ast.CaseClause) {
	// A clause of a break alone is how a case says it does nothing, which the original leaves alone.
	if len(clause.Body) < 2 {
		return
	}
	branch, isBranch := clause.Body[len(clause.Body)-1].(*ast.BranchStmt)
	// A labeled break leaves an enclosing statement, not the switch, so it is no default.
	if !isBranch || branch.Tok != token.BREAK || branch.Label != nil {
		return
	}
	staticcheck_report.Report(context, branch, "redundant break statement", staticcheck_report.FilterGenerated())
}

// checkFunction reports the return that ends body, the body of a function whose type is functionType, when the
// function has no results.
func checkFunction(context *rule.Context, functionType *ast.FuncType, body *ast.BlockStmt) {
	// The original asks whether the results are nil, not whether they are empty, so `func() ()`, which the parser
	// gives an empty list, is a function with results here, and its return is left alone. A declaration without a
	// body, one written in assembly, and a body without statements have no return to report.
	if functionType.Results != nil || body == nil || len(body.List) < 1 {
		return
	}
	// A function of no results returns nothing, so its return carries no values to check.
	returned, isReturn := body.List[len(body.List)-1].(*ast.ReturnStmt)
	if !isReturn {
		return
	}
	staticcheck_report.Report(context, returned, "redundant return statement", staticcheck_report.FilterGenerated())
}
