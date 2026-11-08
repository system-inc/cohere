// Package single_case_select is staticcheck's S1000, a select of one case, which a plain send or receive says
// plainer, and a `for {}` whose body is a select of one receiving case, which a `for range` over the channel says
// plainer, ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1000/s1000.go, whose logic it keeps and whose
// messages it reports word for word, at the same ranges: the whole for statement, and the select keyword. The
// original matches two queries through staticcheck's pattern language, `(ForStmt nil nil nil select@(SelectStmt
// (CommClause (Or (UnaryExpr "<-" _) (AssignStmt _ _ (UnaryExpr "<-" _))) _)))` and `(SelectStmt (CommClause _ _))`;
// the port asks the same of each for and select statement directly, reading pattern/match.go for what each construct
// of the queries matches, and reports through staticcheck_report.Report where the original reports through
// report.Report. The registry runs the port in the original's place, under its name, and the original stays only as the
// oracle: the parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package single_case_select

import (
	"go/ast"
	"go/token"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every for and select statement, in one
// preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.ForStmt)(nil), (*ast.SelectStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		// The check depends on the preorder's order, as the original's seen set does: a for statement comes before
		// every statement nested in it, so the select its finding names is in seen by the time the preorder reaches
		// that select, and the select is not reported a second time. The seen set is the rule's state over the
		// package, the same one on every call there.
		seen := rule.State[Seen](context)
		if covering := Check(context, node, seen); covering != nil {
			seen.record(covering)
		}
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1000",
	"S1000",
	"Flags a select of one case, and a for loop around one that receives: staticcheck's S1000, ported.",
	&Listener,
)

// Check judges one node of the preorder against seen, which it reads and never writes, and returns the select a for
// statement's finding names, for the caller to record in seen, or nil. A for statement with no clauses around a sole
// select of one receiving case is reported, and its select is returned whether or not the report is filtered as
// generated, as the original's seen set takes it before reporting. Any other select of one case is reported at its
// keyword, unless seen holds it.
func Check(context *rule.Context, node ast.Node, seen *Seen) *ast.SelectStmt {
	switch statement := node.(type) {
	case *ast.ForStmt:
		looped := loopedReceive(statement)
		if looped == nil {
			return nil
		}
		staticcheck_report.Report(context, statement, "should use for range instead of for { select {} }",
			staticcheck_report.FilterGenerated())
		return looped
	case *ast.SelectStmt:
		if !hasOneClause(statement) || seen.selects[statement] {
			return nil
		}
		staticcheck_report.Report(context, statement,
			"should use a simple channel send/receive instead of select with a single case",
			staticcheck_report.ShortRange(),
			staticcheck_report.FilterGenerated())
		return nil
	default:
		// Invariant: the dispatcher hands Node, and Node Check, only the for and select statements it was asked for.
		panic("single_case_select: the dispatcher handed over a node that is neither a for nor a select statement")
	}
}

// loopedReceive is the select of the first query, `(ForStmt nil nil nil select@(SelectStmt (CommClause (Or ...)
// _)))`, the one forStatement's body holds when it matches, and nil when it does not.
func loopedReceive(forStatement *ast.ForStmt) *ast.SelectStmt {
	// `nil nil nil` is no init, condition or post statement: the pattern's nil matches an absent one alone, and a
	// parenthesized condition is still one.
	if forStatement.Init != nil || forStatement.Cond != nil || forStatement.Post != nil {
		return nil
	}
	selectStatement, isSelect := soleStatement(forStatement.Body).(*ast.SelectStmt)
	if !isSelect {
		return nil
	}
	clause, isClause := soleClause(selectStatement)
	if !isClause || !receives(clause.Comm) {
		return nil
	}
	return selectStatement
}

// soleStatement is the one statement block holds, as the pattern matches a block against a node that is no list: by
// its statements, of which there must be exactly one. The matcher unwraps a labeled statement to the statement it
// labels and a block to its statements wherever either stands, so the sole statement is seen through any labels on it
// and any blocks of one statement around it, and the select bound is the select itself, never its label. A block of
// none or of more than one statement, at any depth, holds no sole statement, and the result is nil.
func soleStatement(block *ast.BlockStmt) ast.Stmt {
	// A nil block matches only the pattern's nil, and the select pattern is not it.
	if block == nil || len(block.List) != 1 {
		return nil
	}
	statement := block.List[0]
	for {
		switch inner := statement.(type) {
		case *ast.LabeledStmt:
			statement = inner.Stmt
		case *ast.BlockStmt:
			if len(inner.List) != 1 {
				return nil
			}
			statement = inner.List[0]
		default:
			// The matcher unwraps an expression statement and a declaration statement too, to what is never a select.
			return statement
		}
	}
}

// hasOneClause is whether selectStatement matches the second query, `(SelectStmt (CommClause _ _))`: a body of
// exactly one clause, whatever it communicates, a send, a receive or nothing at all for `default:`, since `_` matches
// a missing statement as it matches any other.
func hasOneClause(selectStatement *ast.SelectStmt) bool {
	_, isClause := soleClause(selectStatement)
	return isClause
}

// soleClause is the one clause of selectStatement's body, and false for a body of none or more than one, as
// `(CommClause _ _)` matches the body's list only when it holds exactly one.
func soleClause(selectStatement *ast.SelectStmt) (*ast.CommClause, bool) {
	if len(selectStatement.Body.List) != 1 {
		return nil, false
	}
	clause, isClause := selectStatement.Body.List[0].(*ast.CommClause)
	return clause, isClause
}

// receives is whether communication matches `(Or (UnaryExpr "<-" _) (AssignStmt _ _ (UnaryExpr "<-" _)))`: a receive
// standing alone, which the matcher reaches through its expression statement, or an assignment, by `=` or `:=` to
// any left side, of exactly one right side that is a receive. A send, or no statement for `default:`, is neither.
func receives(communication ast.Stmt) bool {
	switch communication := communication.(type) {
	case *ast.ExprStmt:
		return isReceive(communication.X)
	case *ast.AssignStmt:
		// A list of right sides matches a pattern that is no list only when it holds exactly one.
		return len(communication.Rhs) == 1 && isReceive(communication.Rhs[0])
	default:
		return false
	}
}

// isReceive is whether expression is a receive, `<-` applied to anything, through any parentheses, as the matcher
// sees through them.
func isReceive(expression ast.Expr) bool {
	unary, isUnary := ast.Unparen(expression).(*ast.UnaryExpr)
	return isUnary && unary.Op == token.ARROW
}
