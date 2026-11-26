// Package guarded_delete is staticcheck's S1033, a call of the builtin delete guarded by a lookup of the very key it
// deletes, `if _, ok := m[key]; ok { delete(m, key) }`, which is `delete(m, key)` written longer, since deleting a key
// a map lacks does nothing, ported as a check on types and syntax that reports through the kit every port of
// staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1033/s1033.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the if statement's keyword. The original matches its
// one query, `(IfStmt (AssignStmt [(Ident "_") ok@(Ident _)] ":=" (IndexExpr m key)) ok [call@(CallExpr (Builtin
// "delete") [m key])] nil)`, through staticcheck's pattern language; the port asks the same of each if statement
// directly, reading pattern/match.go for what each construct of the query matches, and reports through
// staticcheck_report.Report where the original reports through report.Report. Unlike the ports before it, the query's
// bindings hold syntax, not objects, so its second m, its second key and its second ok are compared with the first as
// match.go compares two nodes, field by field through reflection, by staticcheck_code.SameSyntax, the port of that
// comparison every port shares. The registry runs the port in the original's place, under its name, and the original
// stays only as the oracle: the parity tests beside this file run both over the same packages, and the two differ
// nowhere they hold.
package guarded_delete

import (
	"go/ast"
	"go/token"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_language/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_language/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every if statement, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.IfStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		statement, isIf := node.(*ast.IfStmt)
		if !isIf {
			// Invariant: the dispatcher hands Node only the if statements it was asked for.
			panic("guarded_delete: the dispatcher handed over a node that is not an if statement")
		}
		Check(context, statement)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1033",
	"S1033",
	"Flags a delete guarded by a lookup of the key it deletes: staticcheck's S1033, ported.",
	&Listener,
)

// Check judges one if statement: one with no else, or an empty one, whose initializer declares the blank identifier
// and one variable from a map lookup, whose condition is that variable, and whose body is a call of the builtin delete
// on the same map and key, guards what delete already does, and the fix replaces the whole statement with the call.
func Check(context *rule.Context, statement *ast.IfStmt) {
	// The query's trailing `nil` is the else, which match.go meets through what it looks through: an else block is
	// its list of statements, and an empty block's list is a nil slice, which `nil` matches. So an empty else is no
	// else, and an else if or an else that holds anything, a lone semicolon included, is left alone.
	if statement.Else != nil {
		block, isBlock := statement.Else.(*ast.BlockStmt)
		if !isBlock || block.List != nil {
			return
		}
	}
	// `(AssignStmt ...)` is the initializer itself: a simple statement is never parenthesized, labeled or a block, so
	// nothing the pattern looks through can wrap it.
	assignment, isAssignment := statement.Init.(*ast.AssignStmt)
	// `":="` is the token exactly, so `_, ok = m[key]` into a variable declared before is left alone.
	if !isAssignment || assignment.Tok != token.DEFINE {
		return
	}
	// `[(Ident "_") ok@(Ident _)]` is two names exactly, the first the blank identifier. Neither can be parenthesized
	// on the left of `:=`, which the parser refuses, but the pattern would look through it, and so does the port.
	if len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 {
		return
	}
	blank, isBlankIdentifier := ast.Unparen(assignment.Lhs[0]).(*ast.Ident)
	if !isBlankIdentifier || blank.Name != "_" {
		return
	}
	found, isFoundIdentifier := ast.Unparen(assignment.Lhs[1]).(*ast.Ident)
	if !isFoundIdentifier {
		return
	}
	// A lone `(IndexExpr m key)` meets the right side's list as a list of one, through parentheses, and an IndexExpr
	// alone, never an IndexListExpr. The pattern asks nothing of the types: a lookup with two results is a map's, and a
	// generic function's instantiation never has two.
	lookup, isLookup := ast.Unparen(assignment.Rhs[0]).(*ast.IndexExpr)
	if !isLookup {
		return
	}
	// `ok` recalls the identifier the initializer bound, which match.go compares with the condition as two nodes: an
	// identifier, through parentheses, spelled the same, its position and its object never read.
	if !staticcheck_code.SameSyntax(found, statement.Cond) {
		return
	}
	call := loneCall(statement.Body.List)
	// `(Builtin "delete")` is the universe's delete, its name through parentheses, so a delete the code declares
	// itself, in a block or at package scope, is not it.
	if call == nil || len(call.Args) != 2 || !staticcheck_code.IsBuiltin(context, call.Fun, "delete") {
		return
	}
	// `[m key]` recalls the map and the key the lookup bound, each without its parentheses, as two nodes again: the
	// same syntax, not the same values, so a key that calls a function is the same key however often it is called,
	// and two variables spelled alike are one.
	if !staticcheck_code.SameSyntax(ast.Unparen(lookup.X), call.Args[0]) ||
		!staticcheck_code.SameSyntax(ast.Unparen(lookup.Index), call.Args[1]) {
		return
	}
	// The fix prints the call the pattern bound, without the parentheses or the block around it, from its own
	// positions, so a call over several lines keeps its lines, and the comments in the if go with the if.
	staticcheck_report.Report(context, statement, "unnecessary guard around call to delete",
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(edit.Fix("Remove guard", edit.ReplaceWithNode(context.FileSet, statement, call))))
}

// loneCall is the call statements hold, as the pattern's `[call@(CallExpr ...)]` meets an if's body: a list of one
// statement, which match.go looks through while it is an expression statement, a labeled statement, a parenthesized
// expression or a block of one statement, nested in any order, until a call is left. Anything else is no call, and
// nil says so.
func loneCall(statements []ast.Stmt) *ast.CallExpr {
	if len(statements) != 1 {
		return nil
	}
	var node ast.Node = statements[0]
	for {
		switch wrapper := node.(type) {
		case *ast.CallExpr:
			return wrapper
		case *ast.ExprStmt:
			node = wrapper.X
		case *ast.ParenExpr:
			node = wrapper.X
		case *ast.LabeledStmt:
			// A label nothing jumps to is an error the type checker reports, so a package that type-checks holds
			// none here, but the pattern looks through one, and so does the port.
			node = wrapper.Stmt
		case *ast.BlockStmt:
			return loneCall(wrapper.List)
		default:
			return nil
		}
	}
}
