// Package guarded_map_access is staticcheck's S1036, an if that asks whether a map holds a key only to write the key
// either way, `if _, ok := m[k]; ok { m[k] += v } else { m[k] = v }`, where the zero value a missing key reads
// already makes the else branch's write, ported as a check on types and syntax that reports through the kit every
// port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1036/s1036.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the if's header. The original matches one query, an
// Or of three if statements, through staticcheck's pattern language:
//
//	(IfStmt (AssignStmt [(Ident "_") ok@(Ident _)] ":=" indexexpr@(IndexExpr _ _)) ok
//		set@(AssignStmt indexexpr "=" (CallExpr (Builtin "append") indexexpr:values))
//		(AssignStmt indexexpr "=" (CompositeLit _ values)))
//	(IfStmt (AssignStmt [(Ident "_") ok] ":=" indexexpr@(IndexExpr _ _)) ok
//		set@(AssignStmt indexexpr "+=" value) (AssignStmt indexexpr "=" value))
//	(IfStmt (AssignStmt [(Ident "_") ok] ":=" indexexpr@(IndexExpr _ _)) ok
//		set@(IncDecStmt indexexpr "++") (AssignStmt indexexpr "=" (IntegerLiteral "1")))
//
// and skips an index expression that may have side effects. The port asks the same of each if statement directly,
// reading pattern/match.go for what each construct matches, and reports through staticcheck_report.Report where the
// original reports through report.Report. The registry runs the port in the original's place, under its name, and the
// original stays only as the oracle: the parity tests beside this file run both over the same packages.
//
// The port parts from the original three times, where the original is wrong, each recorded in the differences
// fixture. The pattern's CallExpr has no Ellipsis, so the original takes `append(m[k], v...)` against an else of
// `[]T{v}`, which holds v as one element where the append spreads its elements; the guard is needed there, and the
// port, which requires a call with no spread, drops the finding. An append of nothing, `append(m[k])`, against an
// empty literal, `[]T{}`, stores nil for a missing key where the else stores an empty slice, which a comparison with
// nil, reflect and encoding/json all tell apart, so the port drops that finding too. And the original's fix puts the
// bare statement in the if's place, which an if that is another if's else branch cannot be, so `else m[k]++` does not
// parse; there the port's fix puts the if's body in its place, the braces with the one statement, which does.
package guarded_map_access

import (
	"go/ast"
	"go/token"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every if statement, in preorder. The query
// names no symbol, so code.Matches walks the if statements of every package, as this does, and its root is an Or of
// if statements alone, so no other kind of node is asked.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.IfStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, stack []ast.Node) {
		ifStatement, isIf := node.(*ast.IfStmt)
		if !isIf {
			// Invariant: the dispatcher hands Node only the if statements it was asked for.
			panic("guarded_map_access: the dispatcher handed over a node that is not an if statement")
		}
		// An if statement is always inside a function, so the stack holds its parent. The if is another if's else
		// branch where that parent is an if whose else it is, the one way an if holds an if statement, since its
		// header's statement is a simple one and its body a block.
		parent, isParentIf := stack[len(stack)-1].(*ast.IfStmt)
		Check(context, ifStatement, isParentIf && parent.Else == ifStatement)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place. This check filters no generated file, as the original does not.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1036",
	"S1036",
	"Flags a needless check that a map holds a key before writing it: staticcheck's S1036, ported.",
	&Listener,
)

// Check judges one if statement, and isElseBranch is whether it is another if's else branch, which only the fix
// reads. An if whose header declares `_, ok := m[k]` and asks ok, whose body is one write of m[k] and whose else is
// one write of m[k] that the body's write makes of a missing key's zero value, is reported, unless m[k] may have side
// effects, and the fix puts the body's write in the if's place.
func Check(context *rule.Context, ifStatement *ast.IfStmt, isElseBranch bool) {
	index, isGuard := guardedIndex(ifStatement)
	if !isGuard {
		return
	}
	// The body is matched against one statement, and a block matches as its list, so it holds that statement alone.
	// The pattern looks through a labeled statement there and in the else, but a label on a block's only statement,
	// an assignment or an increment, is the target of no jump, since no goto can enter the block from outside it, and
	// an unused label does not compile, so each statement is taken as it is.
	if len(ifStatement.Body.List) != 1 {
		return
	}
	set := ifStatement.Body.List[0]
	// The else is matched against an assignment too: an absent else, an else if, and an empty block never are one.
	elseBlock, isBlock := ifStatement.Else.(*ast.BlockStmt)
	if !isBlock || len(elseBlock.List) != 1 {
		return
	}
	otherwise, isAssignment := elseBlock.List[0].(*ast.AssignStmt)
	if !isAssignment || otherwise.Tok != token.ASSIGN || !writesIndex(otherwise.Lhs, index) {
		return
	}
	if !setCoversOtherwise(context, set, otherwise, index) {
		return
	}
	// The original passes no purity, so every call in the index expression may have side effects, len and cap among
	// them.
	if staticcheck_code.MayHaveSideEffects(context, index, nil) {
		return
	}
	replacement := ast.Node(set)
	if isElseBranch {
		// An else branch is an if or a block, so the original's bare statement there, `else m[k]++`, does not
		// parse. The body is the block holding that statement alone, which is the same write in a form that does.
		replacement = ifStatement.Body
	}
	staticcheck_report.Report(context, ifStatement, "unnecessary guard around map access",
		staticcheck_report.ShortRange(),
		staticcheck_report.Fixes(edit.Fix("Simplify map access",
			edit.ReplaceWithNode(context.FileSet, ifStatement, replacement))))
}

// guardedIndex is the index expression an if's header asks a map about, `_, ok := m[k]` with its condition ok, and
// whether the header is that. Its parentheses are removed, as the pattern's binding of indexexpr removes them.
func guardedIndex(ifStatement *ast.IfStmt) (*ast.IndexExpr, bool) {
	header, isAssignment := ifStatement.Init.(*ast.AssignStmt)
	if !isAssignment || header.Tok != token.DEFINE || len(header.Lhs) != 2 || len(header.Rhs) != 1 {
		return nil, false
	}
	// `(Ident "_")` is an identifier spelled _, through parentheses.
	blank, isIdentifier := ast.Unparen(header.Lhs[0]).(*ast.Ident)
	if !isIdentifier || blank.Name != "_" {
		return nil, false
	}
	// The first alternative binds ok to an identifier and the others to anything; a := declares identifiers alone,
	// so the two are the same wherever the code compiles.
	okIdentifier, isIdentifier := ast.Unparen(header.Lhs[1]).(*ast.Ident)
	if !isIdentifier {
		return nil, false
	}
	// The condition recalls ok, a node and not an object, so it is compared by syntax: the identifier by its name,
	// through any parentheses.
	if !staticcheck_code.SameSyntax(okIdentifier, ifStatement.Cond) {
		return nil, false
	}
	// `indexexpr@(IndexExpr _ _)` against the one right-hand side, through parentheses.
	index, isIndex := ast.Unparen(header.Rhs[0]).(*ast.IndexExpr)
	return index, isIndex
}

// setCoversOtherwise is whether set, the body's one statement, writes into the index what otherwise, the else's
// assignment to it, writes, once a missing key reads the zero value: an append to it of what the else's composite
// literal holds, an addition of what the else assigns, or an increment where the else assigns 1.
func setCoversOtherwise(
	context *rule.Context,
	set ast.Stmt,
	otherwise *ast.AssignStmt,
	index *ast.IndexExpr,
) bool {
	switch set := set.(type) {
	case *ast.AssignStmt:
		if !writesIndex(set.Lhs, index) {
			return false
		}
		switch set.Tok {
		case token.ASSIGN:
			return appendsComposite(context, set, otherwise, index)
		case token.ADD_ASSIGN:
			// `value` binds the whole right-hand side, a list, and the else recalls it, so the two lists are as
			// long and the same expression by expression, by syntax.
			return sameExpressions(set.Rhs, otherwise.Rhs)
		default:
			return false
		}
	case *ast.IncDecStmt:
		if set.Tok != token.INC || !staticcheck_code.SameSyntax(index, set.X) {
			return false
		}
		// `(IntegerLiteral "1")` against the else's right-hand side, one expression, the String "1" against its type
		// and value: a constant that prints as 1, which a float or a type parameter's 1 does too.
		if len(otherwise.Rhs) != 1 {
			return false
		}
		typeAndValue, isLiteral := staticcheck_code.IntegerLiteral(context, otherwise.Rhs[0])
		return isLiteral && typeAndValue.Value.String() == "1"
	default:
		return false
	}
}

// appendsComposite is whether set, `m[k] = ...`, assigns an append to the index of the very elements otherwise,
// `m[k] = T{...}`, writes in a composite literal, by the pattern's `(CallExpr (Builtin "append") indexexpr:values)`
// against `(CompositeLit _ values)`.
func appendsComposite(
	context *rule.Context,
	set *ast.AssignStmt,
	otherwise *ast.AssignStmt,
	index *ast.IndexExpr,
) bool {
	if len(set.Rhs) != 1 || len(otherwise.Rhs) != 1 {
		return false
	}
	call, isCall := ast.Unparen(set.Rhs[0]).(*ast.CallExpr)
	if !isCall || !staticcheck_code.IsBuiltin(context, call.Fun, "append") {
		return false
	}
	// The pattern's CallExpr has no Ellipsis, so the original also takes `append(m[k], v...)` against `T{v}`, which
	// is no guard the zero value covers: the append writes v's elements and the literal v itself as one. The port
	// takes only a call that spreads nothing, and the differences fixture records each finding it drops.
	if call.Ellipsis.IsValid() {
		return false
	}
	// `indexexpr:values` is a list whose head recalls the index and whose tail, the rest of the arguments, binds
	// values, which the literal's elements recall.
	if len(call.Args) == 0 || !staticcheck_code.SameSyntax(index, call.Args[0]) {
		return false
	}
	// With no values, the else's literal is empty, `T{}`, a slice that is not nil, where the append of nothing to a
	// missing key's zero value is nil: the guard tells the two apart, so the port requires a value to append, and
	// the differences fixture records each finding it drops.
	if len(call.Args) == 1 {
		return false
	}
	literal, isLiteral := ast.Unparen(otherwise.Rhs[0]).(*ast.CompositeLit)
	if !isLiteral {
		return false
	}
	return sameExpressions(call.Args[1:], literal.Elts)
}

// writesIndex is whether an assignment's left-hand side is the index expression alone, recalled by syntax, as a
// single node in the pattern matches a list of one.
func writesIndex(left []ast.Expr, index *ast.IndexExpr) bool {
	return len(left) == 1 && staticcheck_code.SameSyntax(index, left[0])
}

// sameExpressions is whether two lists of expressions are as long and the same expression by expression, by syntax,
// as match.go compares a bound list with the list that recalls it.
func sameExpressions(bound []ast.Expr, recalled []ast.Expr) bool {
	if len(bound) != len(recalled) {
		return false
	}
	for position := range bound {
		if !staticcheck_code.SameSyntax(bound[position], recalled[position]) {
			return false
		}
	}
	return true
}
