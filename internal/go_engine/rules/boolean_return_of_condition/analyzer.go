// Package boolean_return_of_condition is staticcheck's S1008, an if at the end of a block that returns true or false
// from its condition and the other after it, which `return <condition>` says plainer, ported as a check on types and
// syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1008/s1008.go, whose logic it keeps and whose
// message it reports word for word, at the same range, the if statement as the block holds it. The original matches
// two queries through staticcheck's pattern language, `(IfStmt nil cond [(ReturnStmt [ret@(Builtin (Or "true"
// "false"))])] nil)` against the block's second to last statement and `(ReturnStmt [ret@(Builtin (Or "true"
// "false"))])` against its last; the port asks the same of each block directly, reading pattern/match.go for what
// each construct of the queries matches, and reports through staticcheck_report.Report where the original reports
// through report.Report.
//
// It differs in one place, and not in what it finds: the original builds every file's comment map as its preorder
// enters the file, and the port builds the map only for a file holding a pair the queries match, at that pair, since
// a comment map depends on its file alone and a matched pair is rare. The port keeps no state across nodes. The
// registry runs the port in the original's place, under its name, and the original stays only as the oracle: the parity
// tests beside this file run both over the same packages, and the two differ nowhere they hold.
package boolean_return_of_condition

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every block, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.BlockStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		block, isBlock := node.(*ast.BlockStmt)
		if !isBlock {
			// Invariant: the dispatcher hands Node only the blocks it was asked for.
			panic("boolean_return_of_condition: the dispatcher handed over a node that is not a block")
		}
		Check(context, block)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1008",
	"S1008",
	"Flags an if that returns true or false from its condition, then the other, as returning the condition: "+
		"staticcheck's S1008, ported.",
	&Listener,
)

// Check judges one block: its last two statements an if with no init or else whose body returns true or false alone,
// and a return of the other, with a comparison or no binary operator at all for the if's condition and no comment on
// either statement, are reported at the if as the block holds it, with the condition to return in their place.
func Check(context *rule.Context, block *ast.BlockStmt) {
	count := len(block.List)
	if count < 2 {
		return
	}
	// Do not flag a series of if statements. The original asks the statement as the block holds it, with no matcher
	// between, so a labeled if before the pair is no if here.
	if count >= 3 {
		if _, isIf := block.List[count-3].(*ast.IfStmt); isIf {
			return
		}
	}
	first, last := block.List[count-2], block.List[count-1]

	ifStatement, isIf := soleStatement(first).(*ast.IfStmt)
	if !isIf {
		return
	}
	// The first nil is no init statement: the parser gives none a statement the matcher could unwrap to nothing.
	if ifStatement.Init != nil || !hasNoElse(ifStatement) {
		return
	}
	// `[(ReturnStmt ...)]` matches the body by its statements, exactly one, seen through labels and blocks of one.
	ifReturned := returnedBoolean(context, soleStatement(ifStatement.Body))
	if ifReturned == nil {
		return
	}
	lastReturned := returnedBoolean(context, soleStatement(last))
	if lastReturned == nil {
		return
	}

	// `cond` binds the condition with its parentheses removed, as the matcher unwraps before it stores, so a
	// parenthesized `&&` is still refused and the message renders no parentheses around it.
	condition := ast.Unparen(ifStatement.Cond)
	if binary, isBinary := condition.(*ast.BinaryExpr); isBinary {
		switch binary.Op {
		case token.EQL, token.LSS, token.GTR, token.NEQ, token.LEQ, token.GEQ:
		default:
			return
		}
	}

	// We want the function to return true and false, not the same value both times.
	if ifReturned.Name == lastReturned.Name {
		return
	}

	// Don't flag if either branch is commented.
	file := staticcheck_report.File(context, first)
	if file == nil {
		// Invariant: the inspector walks the package's files, so every block it hands over is in one of them.
		panic("boolean_return_of_condition: a block in none of the package's files")
	}
	commentMap := ast.NewCommentMap(context.Fset, file, file.Comments)
	if isCommented(commentMap, first) || isCommented(commentMap, last) {
		return
	}

	returned := condition
	if ifReturned.Name == "false" {
		returned = negated(context, condition)
	}
	// Each returned value is the bound identifier, which renders as its name.
	staticcheck_report.Report(context, first,
		fmt.Sprintf("should use 'return %s' instead of 'if %s { return %s }; return %s'",
			staticcheck_report.Render(context, returned),
			staticcheck_report.Render(context, condition), ifReturned.Name, lastReturned.Name),
		staticcheck_report.FilterGenerated())
}

// soleStatement is statement as the matcher reaches a pattern of one statement through it: a labeled statement is
// the statement it labels, and a block is its statements, of which there must be exactly one, at any depth. A block
// of none or of more than one holds no sole statement, and the result is nil. The matcher unwraps an expression
// statement and a declaration statement too, to what is never an if or a return, so those are left as they are.
func soleStatement(statement ast.Stmt) ast.Stmt {
	// reached is the statement the unwrapping has come to so far, statement itself until a label or block is seen.
	reached := statement
	for {
		switch inner := reached.(type) {
		case *ast.LabeledStmt:
			reached = inner.Stmt
		case *ast.BlockStmt:
			if len(inner.List) != 1 {
				return nil
			}
			reached = inner.List[0]
		default:
			return reached
		}
	}
}

// hasNoElse is whether ifStatement's else matches the query's last nil: none, or an empty block, since the matcher
// compares a block's statements, and an empty block's list is the nil slice the pattern's nil matches. An else if,
// or an else of any statement, is one.
func hasNoElse(ifStatement *ast.IfStmt) bool {
	if ifStatement.Else == nil {
		return true
	}
	elseBlock, isBlock := ifStatement.Else.(*ast.BlockStmt)
	return isBlock && elseBlock.List == nil
}

// returnedBoolean is the identifier statement returns when it matches `(ReturnStmt [ret@(Builtin (Or "true"
// "false"))])`, and nil when it does not: a return of exactly one result, the predeclared true or false, through any
// parentheses, which the binding holds removed. A true or false the code declares itself, in a block or at package
// scope, is not one, so a constant that happens to be true is never matched.
func returnedBoolean(context *rule.Context, statement ast.Stmt) *ast.Ident {
	returnStatement, isReturn := statement.(*ast.ReturnStmt)
	if !isReturn || len(returnStatement.Results) != 1 {
		return nil
	}
	result := returnStatement.Results[0]
	if !staticcheck_code.IsBuiltin(context, result, "true") && !staticcheck_code.IsBuiltin(context, result, "false") {
		return nil
	}
	identifier, isIdentifier := ast.Unparen(result).(*ast.Ident)
	if !isIdentifier {
		// Invariant: IsBuiltin holds only of an identifier under its parentheses.
		panic("boolean_return_of_condition: a builtin result that is no identifier")
	}
	return identifier
}

// isCommented is whether any comment the comment map associates with node or any node inside it is not one of
// staticcheck's `//@ diag` notes, as the original asks of the map filtered to node. Its test fixtures mark expected
// findings with those notes, which the original ignores so the check can be tested, and the port ignores them alike.
func isCommented(commentMap ast.CommentMap, node ast.Node) bool {
	commented := false
	ast.Inspect(node, func(inner ast.Node) bool {
		if commented {
			return false
		}
		for _, group := range commentMap[inner] {
			for _, comment := range group.List {
				if !strings.HasPrefix(comment.Text, "//@ diag") {
					commented = true
					return false
				}
			}
		}
		return true
	})
	return commented
}

// negated is expression's opposite as the original writes it: a comparison with its operator reversed, an operand
// of the kinds that print bare with a `!` before it, a `!` taken away, and anything else in parentheses after one.
// It builds new nodes and never changes expression's.
func negated(context *rule.Context, expression ast.Expr) ast.Expr {
	switch expression := expression.(type) {
	case *ast.BinaryExpr:
		// The original copies the expression and sets its operator, which this literal spells field by field.
		return &ast.BinaryExpr{
			X:     expression.X,
			OpPos: expression.OpPos,
			Op:    negatedOperator(context, expression),
			Y:     expression.Y,
		}
	case *ast.Ident, *ast.CallExpr, *ast.IndexExpr, *ast.StarExpr:
		return &ast.UnaryExpr{Op: token.NOT, X: expression}
	case *ast.UnaryExpr:
		if expression.Op == token.NOT {
			return expression.X
		}
		return &ast.UnaryExpr{Op: token.NOT, X: expression}
	default:
		return &ast.UnaryExpr{Op: token.NOT, X: &ast.ParenExpr{X: expression}}
	}
}

// negatedOperator is the comparison opposite comparison's operator, and the operator itself for any other, which
// Check never hands over.
func negatedOperator(context *rule.Context, comparison *ast.BinaryExpr) token.Token {
	switch comparison.Op {
	case token.EQL:
		return token.NEQ
	case token.LSS:
		return token.GEQ
	case token.GTR:
		// Some builtins never return negative ints; "len(x) <= 0" should be "len(x) == 0". The original asks the
		// left side as written, so a parenthesized call there is no call, and the right side as code.IsIntegerLiteral
		// does, an integer literal through parentheses and signs whose value compares equal to 0.
		if call, isCall := comparison.X.(*ast.CallExpr); isCall &&
			staticcheck_code.IsCallToAny(context, call, "len", "cap", "copy") &&
			staticcheck_code.IsIntegerLiteral(context, comparison.Y, constant.MakeInt64(0)) {
			return token.EQL
		}
		return token.LEQ
	case token.NEQ:
		return token.EQL
	case token.LEQ:
		return token.GTR
	case token.GEQ:
		return token.LSS
	default:
		return comparison.Op
	}
}
