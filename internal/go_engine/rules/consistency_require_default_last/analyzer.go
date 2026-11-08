// Package consistency_require_default_last is the Go engine's rule that a switch, type switch or select keeps its
// default clause last.
//
// Go picks the default only when no other clause matches, wherever the default sits, so its place says nothing to
// the program and everything to the reader: a reader looks for the fallback at the bottom, after every case it stands
// behind. staticcheck's ST1015 flags a default in the middle and accepts one first; the house wants it last in both,
// so every switch reads the same way down.
package consistency_require_default_last

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-require-default-last"

// listener hears every switch, type switch and select of the syntax dispatcher's walk.
var listener = rule.Listener{
	Nodes: []ast.Node{(*ast.SwitchStmt)(nil), (*ast.TypeSwitchStmt)(nil), (*ast.SelectStmt)(nil)},
	Node:  judgeStatement,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_require_default_last", "Flags a default clause that is not "+
	"the last clause of its switch, type switch or select, since Go picks the default only when nothing else matches "+
	"and a reader looks for that fallback at the bottom. A default that falls through into the next clause, or that "+
	"the clause before falls through into, is left alone, since there its place is the program's meaning.", &listener)

// Rule is this rule's entry in the Go engine's registry. It reads only the order of clauses in the syntax tree.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer, Listener: &listener}

var defaultNotLastMessage = policy.MessageOf(Name, "defaultNotLast")

// judgeStatement judges node's own clauses. Nested statements are reached by the walk going on, so each is judged on
// its own clauses.
func judgeStatement(context *rule.Context, node ast.Node, _ []ast.Node) {
	switch statement := node.(type) {
	case *ast.SwitchStmt:
		judgeSwitch(context, statement.Body, "switch")
	case *ast.TypeSwitchStmt:
		judgeSwitch(context, statement.Body, "type switch")
	case *ast.SelectStmt:
		judgeSelect(context, statement.Body)
	}
}

// judgeSwitch flags the default clause of an expression or type switch whose body is body when it is not the last
// clause. kind names the statement in the message.
func judgeSwitch(context *rule.Context, body *ast.BlockStmt, kind string) {
	clauses := body.List
	for index, statement := range clauses {
		// The parser puts only case clauses in a switch's body, so anything else is skipped rather than guessed at.
		clause, isClause := statement.(*ast.CaseClause)
		// A clause with no expressions is the default; `case:` with nothing after it does not parse.
		if !isClause || clause.List != nil || index == len(clauses)-1 {
			continue
		}
		// A fallthrough out of the default, or into it from the clause before, ties the default to its neighbour, so
		// moving it last would change which statements run. Only an expression switch can fall through.
		if fallsThrough(clause) || index > 0 && fallsThrough(clauses[index-1]) {
			continue
		}
		report(context, clause.Case, clause.Colon, kind)
	}
}

// judgeSelect flags the default clause of a select whose body is body when it is not the last clause.
func judgeSelect(context *rule.Context, body *ast.BlockStmt) {
	clauses := body.List
	for index, statement := range clauses {
		// The parser puts only communication clauses in a select's body, so anything else is skipped.
		clause, isClause := statement.(*ast.CommClause)
		// A clause with no send or receive is the default.
		if !isClause || clause.Comm != nil || index == len(clauses)-1 {
			continue
		}
		report(context, clause.Case, clause.Colon, "select")
	}
}

// fallsThrough is whether statement is a case clause whose last statement, past any empty ones a stray semicolon
// leaves, is fallthrough. The spec allows fallthrough only as a clause's final statement, so a fallthrough inside a
// nested block or switch is never this clause's and is never read.
func fallsThrough(statement ast.Stmt) bool {
	clause, isClause := statement.(*ast.CaseClause)
	if !isClause {
		return false
	}
	for index := len(clause.Body) - 1; index >= 0; index-- {
		if _, isEmpty := clause.Body[index].(*ast.EmptyStmt); isEmpty {
			continue
		}
		branch, isBranch := clause.Body[index].(*ast.BranchStmt)
		return isBranch && branch.Tok == token.FALLTHROUGH
	}
	return false
}

// report flags the default clause that opens at keyword with its colon at colon, so the finding covers `default:`
// and not the clause's body. kind names the statement it belongs to.
func report(context *rule.Context, keyword token.Pos, colon token.Pos, kind string) {
	context.Report(analysis.Diagnostic{
		Pos:      keyword,
		End:      colon + 1,
		Category: defaultNotLastMessage.Id,
		Message:  defaultNotLastMessage.Render(map[string]string{"statement": kind}),
	})
}
