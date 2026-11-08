// Package sliding_copy is staticcheck's S1018, a counted loop that slides a slice's elements down to its start one by
// one, `for i := 0; i < n; i++ { bs[i] = bs[offset+i] }`, which `copy(bs[:n], bs[offset:])` says in one call, since
// copy permits a source and a destination that overlap, ported as a check on types and syntax that reports through
// the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1018/s1018.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the loop's header. The original matches its one query
// through staticcheck's pattern language:
//
//	(ForStmt (AssignStmt initvar@(Ident _) _ (IntegerLiteral "0")) (BinaryExpr initvar "<" limit@(Ident _))
//		(IncDecStmt initvar "++")
//		[(AssignStmt (IndexExpr slice@(Ident _) initvar) "="
//			(IndexExpr slice (BinaryExpr offset@(Ident _) "+" initvar)))])
//
// and builds its fix from `(CallExpr (Ident "copy") [(SliceExpr slice nil limit nil) (SliceExpr slice offset nil
// nil)])` through code.EditMatch. The port asks the same of each for statement directly, reading pattern/match.go for
// what each construct of the query matches, builds the fix's syntax itself as pattern.NodeToAST builds it, around the
// loop's own identifiers, and reports through staticcheck_report.Report where the original reports through
// report.Report. The query names no symbol, so code.Matches walks every package and the port needs no guard for the
// package under analysis. The registry runs the port in the original's place, under its name, and the original stays
// only as the oracle: the parity tests beside this file run both over the same packages, and the two differ only where
// the original is wrong and the port right, each recorded in the differences fixture: the port drops a loop whose
// counter starts where it stood or doubles as the offset or the limit, and withholds the fix where copy is not the
// builtin or the counter outlives the loop.
package sliding_copy

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every for statement, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.ForStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		loop, isLoop := node.(*ast.ForStmt)
		if !isLoop {
			// Invariant: the dispatcher hands Node only the for statements it was asked for.
			panic("sliding_copy: the dispatcher handed over a node that is not a for statement")
		}
		Check(context, loop)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1018",
	"S1018",
	"Flags a loop that slides a slice's elements down one by one, which a call to copy says: "+
		"staticcheck's S1018, ported.",
	&Listener,
)

// Check judges one for statement: a counter set to the integer literal zero, run while it is less than a limit named
// by an identifier and incremented, around a body of one assignment that stores at the counter an element of the same
// slice read at an offset named by an identifier plus the counter, slides the slice's elements down, and the fix
// calls copy over the two ends of the slice in the loop's place. Where the original's query flags a loop that is no
// slide, or suggests a call that names no builtin or drops the counter's last value, the port parts from it, each
// case said where it is judged.
func Check(context *rule.Context, loop *ast.ForStmt) {
	// The initializer is an assignment, never one wrapped in anything, since the pattern looks through no wrapping a
	// simple statement can take but an expression statement's, which holds no assignment.
	initializer, isAssignment := loop.Init.(*ast.AssignStmt)
	if !isAssignment || len(initializer.Lhs) != 1 || len(initializer.Rhs) != 1 {
		return
	}
	// The query's `_` operator admits every assignment, and the original flags `i += 0` and `i |= 0`, whose counter
	// starts where it stood, so the loop slides from there and the fix would move the slice's first elements too. The
	// port drops those, keeping the operators that leave the counter zero: := and =, and *= and &=, which zero any
	// counter. Where the two part is recorded in the differences fixture.
	switch initializer.Tok {
	case token.DEFINE, token.ASSIGN, token.MUL_ASSIGN, token.AND_ASSIGN:
	default:
		return
	}
	// `initvar@(Ident _)` binds the identifier without the parentheses the pattern looks through, `(i) = 0`.
	counter, isCounter := ast.Unparen(initializer.Lhs[0]).(*ast.Ident)
	if !isCounter {
		return
	}
	// `(IntegerLiteral "0")` is an integer literal whose constant prints as 0, so 0x0 and -0 are it, and a named
	// constant or a conversion of zero is not.
	zero, isLiteral := staticcheck_code.IntegerLiteral(context, initializer.Rhs[0])
	if !isLiteral || zero.Value.String() != "0" {
		return
	}

	// An absent condition is no binary expression, and the counter recalled is the left side by its spelling, as
	// the pattern compares a node it bound, so `n > i` is not the query's shape.
	condition, isBinary := ast.Unparen(loop.Cond).(*ast.BinaryExpr)
	if !isBinary || condition.Op != token.LSS || !staticcheck_code.SameSyntax(counter, condition.X) {
		return
	}
	// `limit@(Ident _)`: a length that is a call, a selector or arithmetic is left alone.
	limit, isLimit := ast.Unparen(condition.Y).(*ast.Ident)
	if !isLimit {
		return
	}

	increment, isIncrement := loop.Post.(*ast.IncDecStmt)
	if !isIncrement || increment.Tok != token.INC || !staticcheck_code.SameSyntax(counter, increment.X) {
		return
	}

	slice, offset, isSlide := slidAssignment(loop.Body, counter)
	if !isSlide {
		return
	}
	// The query binds the offset and the limit to any identifier, the counter's own among them, and the original
	// flags both: with the counter as the offset, `bs[i] = bs[i+i]`, the loop gathers every second element, and with
	// it as the limit, `i < i`, the loop never runs, so neither is a slide, and the fix would name a counter its
	// header declared where it is declared no longer. The port drops both, by object, though the body of one
	// statement declares nothing, so an identifier spelled as the counter is the counter.
	counterObject := context.TypesInfo.ObjectOf(counter)
	if context.TypesInfo.ObjectOf(offset) == counterObject || context.TypesInfo.ObjectOf(limit) == counterObject {
		return
	}
	// The query never asks the slice's type, so the original reads it of the identifier the query bound, and every
	// type in its type set must be a slice: an array, a pointer to one, a map, or a type parameter that admits any of
	// them, is left alone. An identifier with no type is one only a package that fails to type-check holds.
	sliceType := context.TypesInfo.TypeOf(slice)
	if sliceType == nil || !typeutil.All(sliceType, typeutil.IsSlice) {
		return
	}

	// The fix's call holds the bound identifiers themselves, the slice's from the stored element in both arguments,
	// not copies, as pattern.NodeToAST places a binding's value, so format prints them as the original's fix does.
	call := &ast.CallExpr{
		Fun:  &ast.Ident{Name: "copy"},
		Args: []ast.Expr{&ast.SliceExpr{X: slice, High: limit}, &ast.SliceExpr{X: slice, Low: offset}},
	}
	fixes := []analysis.SuggestedFix{}
	// The call names copy as written, the pattern's `(Ident "copy")`, and the original suggests it even where the
	// loop stands in the scope of a copy of the code's own, a variable, a function or an imported package's name,
	// where it fails to compile or calls something else. No spelling names the builtin there, so the port keeps the
	// finding and withholds the fix.
	//
	// The loop also leaves its counter at the limit, and a counter declared outside the for statement, assigned by =,
	// *= or &=, outlives it, so the original's fix changes what the code after the loop reads. Whether anything reads
	// it is a question of flow the port does not ask: the declaration's place is exact, so the port withholds the fix
	// for every counter the header does not declare, and keeps the finding, since the loop is still a slide.
	if namesBuiltinCopy(context, loop) && isDeclaredIn(counterObject, loop) {
		fixes = append(fixes, edit.Fix("Use copy() instead of loop", edit.ReplaceWithNode(context.Fset, loop, call)))
	}
	staticcheck_report.Report(context, loop, "should use copy() instead of loop for sliding slice elements",
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(fixes...))
}

// isDeclaredIn is whether object is declared inside loop, as a counter its header declares with := is, so it ends
// with the loop. An identifier with no object is one only a package that fails to type-check holds, and counts as
// declared outside.
func isDeclaredIn(object types.Object, loop *ast.ForStmt) bool {
	return object != nil && object.Pos() >= loop.Pos() && object.Pos() < loop.End()
}

// namesBuiltinCopy is whether copy, spelled alone where the loop stands, names the builtin: no scope around the loop
// declares a copy before it, a package's declarations counting from anywhere in it. The loop's own scope holds only
// what its header declares, from the header's end on, so a counter named copy is no copy where the call takes the
// loop's place.
func namesBuiltinCopy(context *rule.Context, loop *ast.ForStmt) bool {
	scope := context.Package.Scope().Innermost(loop.Pos())
	if scope == nil {
		// Invariant: a for statement stands in a function's body, whose scope the type checker records around it.
		panic("sliding_copy: no scope holds a for statement")
	}
	_, object := scope.LookupParent("copy", loop.Pos())
	return object == types.Universe.Lookup("copy")
}

// slidAssignment is the slice and the offset the body binds when it is `[(AssignStmt (IndexExpr slice@(Ident _)
// initvar) "=" (IndexExpr slice (BinaryExpr offset@(Ident _) "+" initvar)))]`: one assignment of one element of a slice
// named by an identifier, read at the offset plus the counter, to the same slice at the counter. Each identifier is
// bound without its parentheses, and the slice and the counter recalled are compared by spelling, as the pattern
// compares a node it bound, so the offset must come first in the sum, and `bs[i+offset]` is not the query's shape.
func slidAssignment(body *ast.BlockStmt, counter *ast.Ident) (slice *ast.Ident, offset *ast.Ident, isSlide bool) {
	assignment, isAssignment := soleAssignment(body.List)
	if !isAssignment || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil, nil, false
	}
	// `(IndexExpr ...)` is an index of one, never an instantiation's list of type arguments.
	stored, isStored := ast.Unparen(assignment.Lhs[0]).(*ast.IndexExpr)
	if !isStored || !staticcheck_code.SameSyntax(counter, stored.Index) {
		return nil, nil, false
	}
	slice, isSlice := ast.Unparen(stored.X).(*ast.Ident)
	if !isSlice {
		return nil, nil, false
	}
	read, isRead := ast.Unparen(assignment.Rhs[0]).(*ast.IndexExpr)
	if !isRead || !staticcheck_code.SameSyntax(slice, read.X) {
		return nil, nil, false
	}
	sum, isSum := ast.Unparen(read.Index).(*ast.BinaryExpr)
	if !isSum || sum.Op != token.ADD || !staticcheck_code.SameSyntax(counter, sum.Y) {
		return nil, nil, false
	}
	offset, isOffset := ast.Unparen(sum.X).(*ast.Ident)
	if !isOffset {
		return nil, nil, false
	}
	return slice, offset, true
}

// soleAssignment is the assignment a list of one statement holds, looking through labels and blocks of one
// statement on the way, as match.go looks through them before comparing a statement with the pattern's AssignStmt.
// A list of any other length, an empty block among them, holds none.
func soleAssignment(statements []ast.Stmt) (*ast.AssignStmt, bool) {
	if len(statements) != 1 {
		return nil, false
	}
	statement := statements[0]
	for {
		switch wrapped := statement.(type) {
		case *ast.AssignStmt:
			return wrapped, true
		case *ast.LabeledStmt:
			statement = wrapped.Stmt
		case *ast.BlockStmt:
			if len(wrapped.List) != 1 {
				return nil, false
			}
			statement = wrapped.List[0]
		default:
			return nil, false
		}
	}
}
