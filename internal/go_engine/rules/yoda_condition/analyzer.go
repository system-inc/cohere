// Package yoda_condition is staticcheck's `ST1017`, a Yoda condition, an equality comparison that puts the constant
// first, `if 42 == x`, where Go, which has no assignment inside a condition to guard against, reads `if x == 42`,
// ported onto the engine's syntax dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1017/st1017.go, whose message and fix it
// reports word for word, at the comparison, with no range option, and withholds in a generated file as the original's
// FilterGenerated does. The original matches staticcheck's pattern `(BinaryExpr left@(TrulyConstantExpression _)
// tok@(Or "==" "!=") right@(Not (TrulyConstantExpression _)))` and writes its fix from `(BinaryExpr right tok left)`.
// The port reads the comparison's syntax and types directly: the pattern's matcher looks through any parentheses
// around an operand before it judges or binds it, so the port judges and swaps each operand with its parentheses
// removed, and builds the swapped comparison as the pattern's NodeToAST does, its operator's position left unset, and
// prints it with go/format over the package's file set, as the original's EditMatch does. The original requires the
// inspector and x/tools' type index for its pattern machinery; the port requires neither, hearing each comparison on
// the dispatcher's walk.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge.
package yoda_condition

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1017"

// Doc is the original's title.
const Doc = "Don't use Yoda conditions"

// message is the original's message, word for word.
const message = "don't use Yoda conditions"

// fixMessage is the original's name for its fix, word for word.
const fixMessage = "Un-Yoda-fy"

// Listener is what the port hears of each package from the dispatcher: every binary expression, of which it judges
// the equality comparisons.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.BinaryExpr)(nil)},
	Node:  checkComparison,
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// checkComparison reports node when it compares for equality or inequality a truly constant expression on the left
// with one that is not on the right, with the fix that swaps them.
func checkComparison(context *rule.Context, node ast.Node, _ []ast.Node) {
	comparison, isBinary := node.(*ast.BinaryExpr)
	if !isBinary || (comparison.Op != token.EQL && comparison.Op != token.NEQ) {
		return
	}
	// The pattern's matcher strips every parenthesis around an operand before matching it, and binds the operand it
	// matched, so `(0) == (y)` is judged and swapped as `0 == y` is.
	left := ast.Unparen(comparison.X)
	right := ast.Unparen(comparison.Y)
	if !isTrulyConstant(context, left) || isTrulyConstant(context, right) {
		return
	}
	swapped := &bytes.Buffer{}
	// The pattern's NodeToAST sets the expression's operands and operator by name and leaves its operator's position
	// unset, which the printer reads when it decides where to break a line, so the port leaves it unset too.
	printError := format.Node(swapped, context.Fset, &ast.BinaryExpr{X: right, Op: comparison.Op, Y: left})
	if printError != nil {
		// Invariant: go/format prints any expression the parser built into a buffer, which never fails to take a
		// write, so an error is a fault in the printer, and the dispatcher's recover makes it this rule's crash on
		// the package rather than a fix with nothing in it.
		panic(fmt.Sprintf("printing the swapped comparison: %v", printError))
	}
	staticcheck_report.Report(context, comparison, message,
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(analysis.SuggestedFix{
			Message: fixMessage,
			TextEdits: []analysis.TextEdit{{
				Pos:     comparison.Pos(),
				End:     comparison.End(),
				NewText: swapped.Bytes(),
			}},
		}))
}

// isTrulyConstant is whether expression is a constant written without a single identifier, as staticcheck's pattern
// TrulyConstantExpression matches: a literal, or literals under operators and parentheses, such as 42, "", 1<<3 or
// -(2+3). A named constant, true and false, nil, a conversion and a call to len are each written with an identifier,
// so none is truly constant, whatever its value.
func isTrulyConstant(context *rule.Context, expression ast.Expr) bool {
	typeAndValue, isRecorded := context.TypesInfo.Types[expression]
	if !isRecorded || typeAndValue.Value == nil {
		return false
	}
	namesIdentifier := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if _, isIdentifier := node.(*ast.Ident); isIdentifier {
			namesIdentifier = true
		}
		return !namesIdentifier
	})
	return !namesIdentifier
}
