// Package consistency_no_useless_concatenation is the Go engine's rule that two string literals are not joined with +
// on one line.
//
// It is the Go form of ESLint core's no-useless-concat, and follows the core port's reading of it: the operands are
// the ones beside the operator, found by descending into a + on either side, parentheses are seen through, and a
// join that crosses a line break is how a long string is wrapped and is left alone. Nothing is computed between two
// literals, so the join only adds an operator for a reader to follow and a seam where the halves can drift apart.
//
// Go has two kinds of string literal, and they hold different text: a raw one cannot hold a backtick and reads no
// escapes, and an interpreted one needs escapes for a quote, a backslash and a newline. Joining one of each is how Go
// spells a string neither kind holds cleanly, a backtick inside raw text being the common case, so a join across
// kinds is reported only when the two texts, pasted together, are already one literal of either kind.
package consistency_no_useless_concatenation

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-useless-concatenation"

// listener reads the package's line table as the syntax dispatcher's walk enters its first file, and hears every
// binary expression of the walk.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{positions.Analyzer},
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.BinaryExpr)(nil)},
	Node:      judgeBinary,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_useless_concatenation", "Flags a + between two string "+
	"literals on one line, since nothing is computed between them and one literal says the same with no operator to "+
	"follow. A join across lines, which is how a long string is wrapped, is left alone, and so is a raw literal "+
	"joined to an interpreted one whose texts cannot be pasted into one.", &listener)

// Rule is this rule's entry in the Go engine's registry. It reads no type information: a + with a string literal
// beside it is string concatenation in any package that compiles, so the literals' syntax is the whole judgment.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer, Listener: &listener}

var uselessConcatenationMessage = policy.MessageOf(Name, "uselessConcatenation")

// packageState is what the rule reads once for its package, held in its context's state.
type packageState struct {
	// table is the package's line table, read as the walk enters its first file.
	table *positions.Table
}

// enterFile reads the package's line table, the first time the walk enters one of its files.
func enterFile(context *rule.Context, _ *ast.File) {
	state := rule.State[packageState](context)
	if state.table != nil {
		return
	}
	table, isTable := context.ResultOf(positions.Analyzer).(*positions.Table)
	if !isTable {
		// Invariant: positions' result is a *Table, its declared ResultType, which the checker holds it to. Anything
		// else is a mistake in positions, and the dispatcher's recover makes it this rule's crash on the package.
		panic(fmt.Sprintf("reading the line table: positions gave %T", context.ResultOf(positions.Analyzer)))
	}
	state.table = table
}

// judgeBinary reports a + between two string literals that stand on one line.
func judgeBinary(context *rule.Context, node ast.Node, _ []ast.Node) {
	binary, isBinary := node.(*ast.BinaryExpr)
	if !isBinary {
		return
	}
	left, right, joins := JoinedLiterals(binary)
	if !joins || !onOneLine(rule.State[packageState](context).table, left, right) {
		return
	}
	// The operator rather than the expression, as the core port reports, so a chain's findings point at the one join
	// each is about.
	context.Report(analysis.Diagnostic{
		Pos:      binary.OpPos,
		End:      binary.OpPos + token.Pos(len(token.ADD.String())),
		Category: uselessConcatenationMessage.Id,
		Message:  uselessConcatenationMessage.Render(map[string]string{}),
	})
}

// JoinedLiterals are the two string literals beside binary's operator when binary is a + the rule reports once both
// stand on one line: the literals found by descending into a + on either side through parentheses, whose texts paste
// into one literal. The rule's listener reports through it, and the fix phase's long-line wrap asks it of every + it
// could break after, so it never breaks a join the rule would report, which a join across lines would hide from it.
func JoinedLiterals(binary *ast.BinaryExpr) (left *ast.BasicLit, right *ast.BasicLit, joins bool) {
	if binary.Op != token.ADD {
		return nil, nil, false
	}
	left, leftIsString := stringLiteral(rightmostOfLeft(binary.X))
	right, rightIsString := stringLiteral(leftmostOfRight(binary.Y))
	if !leftIsString || !rightIsString || !pasteIntoOne(left, right) {
		return nil, nil, false
	}
	return left, right, true
}

// rightmostOfLeft is the element of a + operator's left operand that stands beside the operator. A + associates
// left, so name + "a" + "b" holds name + "a" on the left of its outer +, and the literal beside that operator is the
// inner sum's own right operand.
func rightmostOfLeft(expression ast.Expr) ast.Expr {
	current := ast.Unparen(expression)
	for {
		binary, isBinary := current.(*ast.BinaryExpr)
		if !isBinary || binary.Op != token.ADD {
			return current
		}
		current = ast.Unparen(binary.Y)
	}
}

// leftmostOfRight is the mirror of rightmostOfLeft, for a parenthesized sum on the right, "a" + ("b" + c).
func leftmostOfRight(expression ast.Expr) ast.Expr {
	current := ast.Unparen(expression)
	for {
		binary, isBinary := current.(*ast.BinaryExpr)
		if !isBinary || binary.Op != token.ADD {
			return current
		}
		current = ast.Unparen(binary.X)
	}
}

// stringLiteral is expression as a string literal, raw or interpreted. A rune literal is a number, so 'a' + 'b' is
// arithmetic and never a join.
func stringLiteral(expression ast.Expr) (*ast.BasicLit, bool) {
	literal, isLiteral := expression.(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return nil, false
	}
	return literal, true
}

// onOneLine is whether left ends on the line right starts on, the core port's test. The lines are the file's own, not
// those a //line directive gives it, since a directive between the operands would otherwise move one of them onto
// another's line or off it. table reads both lines without the FileSet's lock, which every join would take.
func onOneLine(table *positions.Table, left *ast.BasicLit, right *ast.BasicLit) bool {
	return table.Line(left.End()) == table.Line(right.Pos())
}

// pasteIntoOne is whether the two literals' texts, set side by side, are one literal that holds what the join does,
// so the finding never asks for an escape the author joined to avoid. Two of a kind always are. A raw text becomes
// interpreted unchanged when it has no quote, backslash or line break, and an interpreted text becomes raw unchanged
// when it has no escape and no backtick; a carriage return is refused on either side, since raw text drops it.
func pasteIntoOne(left *ast.BasicLit, right *ast.BasicLit) bool {
	if isRaw(left) == isRaw(right) {
		return true
	}
	raw, interpreted := left, right
	if isRaw(right) {
		raw, interpreted = right, left
	}
	return !strings.ContainsAny(textOf(raw), "\"\\\n\r") || !strings.ContainsAny(textOf(interpreted), "\\`\r")
}

// isRaw is whether literal is a raw string, which the parser leaves opening with its backtick.
func isRaw(literal *ast.BasicLit) bool {
	return strings.HasPrefix(literal.Value, "`")
}

// textOf is literal's source between its delimiters, each of which is one byte for both kinds.
func textOf(literal *ast.BasicLit) string {
	if len(literal.Value) < 2 {
		return ""
	}
	return literal.Value[1 : len(literal.Value)-1]
}
