// Package repeated_if_else_condition is staticcheck's SA4014, a condition an if/else if chain tests twice with nothing
// between that could change it, so the second test can never match where the first did not, ported as a check on
// syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4014/sa4014.go, whose logic it keeps and
// whose message it reports word for word, at the repeated condition's Pos and End. It asks
// staticcheck_code.MayHaveSideEffects where the original asks code.MayHaveSideEffects, and reports through
// staticcheck_report.Report where the original reports through report.Report, each the port of the one the original
// calls, proven against it beside it. Two conditions are the same when staticcheck's report.Render prints them the
// same, and the port calls that Render itself, borrowed in this wave, so the two compare conditions by the very same
// text. It calls it as `report.Render(&analysis.Pass{Fset: context.Fset}, condition)`, a pass holding the file set
// alone, since Render reads nothing else of a pass; the kit's port of Render takes the context in its place.
//
// The original keeps one piece of state across the package: the if statements it has already walked as part of a
// chain, so the else if of a chain is not judged again as a chain of its own. The port keeps the same in the
// context's state for the package, made on the first if the dispatcher hands Node and handed to Check. The registry
// still runs the original, which stays the oracle: the parity tests beside this file run both over the same packages,
// and the two differ nowhere they hold.
package repeated_if_else_condition

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/report"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every if statement, in preorder, outer before
// inner, so a chain is judged from its first if, with the if statements already walked kept in the context's state
// for the package.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.IfStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		ifStatement, isIfStatement := node.(*ast.IfStmt)
		if !isIfStatement {
			// Invariant: the dispatcher hands Node only the if statements it was asked for.
			panic("repeated_if_else_condition: the dispatcher handed over a node that is not an if statement")
		}
		walked := rule.State[map[*ast.IfStmt]bool](context)
		if *walked == nil {
			*walked = map[*ast.IfStmt]bool{}
		}
		Check(context, ifStatement, *walked)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4014",
	"SA4014",
	"Flags a condition an if/else if chain repeats with no side effects between, which can never match the "+
		"second time where it did not the first: staticcheck's SA4014, ported as a check on syntax.",
	&Listener,
)

// Check judges the chain ifStatement starts: when every if of it has no initializer and a condition with no side
// effects, each condition the chain tests a second time is reported there. walked holds the if statements already
// walked as part of a chain, which Check skips and adds to, so the else ifs of a chain are not judged again.
//
// Mutates: walked gains each if statement of the chain it walks.
func Check(context *rule.Context, ifStatement *ast.IfStmt, walked map[*ast.IfStmt]bool) {
	if walked[ifStatement] {
		return
	}
	// An if with no else tests one condition, so it repeats none.
	if ifStatement.Else == nil {
		return
	}
	conditions, isPlain := chainConditions(context, ifStatement, walked)
	if !isPlain || len(conditions) < 2 {
		return
	}
	// staticcheck's Render reads nothing of a pass but its file set.
	renderPass := &analysis.Pass{Fset: context.Fset}
	counts := map[string]int{}
	for _, condition := range conditions {
		rendered := report.Render(renderPass, condition)
		counts[rendered]++
		// The second time a condition is tested is reported, and a third time is not reported again.
		if counts[rendered] == 2 {
			staticcheck_report.Report(context, condition,
				"this condition occurs multiple times in this if/else if chain")
		}
	}
}

// chainConditions is the condition of ifStatement and of each else if after it, in order, and true; or false where an
// if of the chain has an initializer or a condition that may have side effects, either of which could change what a
// later condition tests. Each if it reaches, the one it stops at included, is added to walked; the ifs after the one
// it stops at are not, so each is judged later as a chain of its own.
//
// Mutates: walked gains each if statement it reaches.
func chainConditions(
	context *rule.Context,
	ifStatement *ast.IfStmt,
	walked map[*ast.IfStmt]bool,
) ([]ast.Expr, bool) {
	conditions := []ast.Expr{}
	for current := ifStatement; ; {
		walked[current] = true
		// No function is judged pure here, so every call in a condition counts as a side effect.
		if current.Init != nil || staticcheck_code.MayHaveSideEffects(context, current.Cond, nil) {
			return nil, false
		}
		conditions = append(conditions, current.Cond)
		next, isElseIf := current.Else.(*ast.IfStmt)
		if !isElseIf {
			return conditions, true
		}
		current = next
	}
}
