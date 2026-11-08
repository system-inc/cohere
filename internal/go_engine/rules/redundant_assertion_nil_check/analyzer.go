// Package redundant_assertion_nil_check is staticcheck's S1020, a nil check beside a type assertion's ok, `if _, ok :=
// i.(T); ok && i != nil`, or around it, `if i != nil { if _, ok := i.(T); ok {} }`, which says again what ok already
// says, since an assertion on a nil interface never succeeds, ported as a check on types and syntax that reports
// through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1020/s1020.go, whose logic it keeps and whose
// message it reports word for word, at the same ranges: the header of the if whose condition holds the nil check, and
// the header of the inner if of the nested shape. The original matches two queries through staticcheck's pattern
// language, `(IfStmt (AssignStmt [(Ident "_") ok@(Object _)] _ [(TypeAssertExpr assert@(Object _) _)]) (Or
// (BinaryExpr ok "&&" (BinaryExpr assert "!=" (Builtin "nil"))) (BinaryExpr (BinaryExpr assert "!=" (Builtin "nil"))
// "&&" ok)) _ _)` and `(IfStmt nil (BinaryExpr lhs@(Object _) "!=" (Builtin "nil")) [ifstmt@(IfStmt (AssignStmt
// [(Ident "_") ok@(Object _)] _ [(TypeAssertExpr lhs _)]) ok _ nil)] nil)`; the port asks the same of each if statement
// directly, reading pattern/match.go for what each construct of the queries matches, and reports through
// staticcheck_report.Report where the original reports through report.Report. The original reports every finding of
// its first query in the package before any of its second; the port reports each if's findings as its preorder meets
// it, the same findings in another order. The registry still runs the original, which stays the oracle: the parity
// tests beside this file run both over the same packages, and the two differ nowhere they hold.
package redundant_assertion_nil_check

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, since this check filters them.
var Analyzer = &analysis.Analyzer{
	Name:     "S1020",
	Doc:      "Flags a nil check that a type assertion's ok already makes: staticcheck's S1020, ported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every if statement of the package, the root of both queries, in one
// preorder.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("redundant_assertion_nil_check: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.IfStmt)(nil)}, func(node ast.Node) {
		ifStatement, isIf := node.(*ast.IfStmt)
		if !isIf {
			// Invariant: Preorder hands this function only the if statements it was asked for.
			panic("redundant_assertion_nil_check: the inspector handed over a node that is not an if statement")
		}
		Check(context, ifStatement)
	})
	return nil, nil
}

// Check judges one if statement against both queries: an if that asserts in its initializer and checks the asserted
// variable against nil beside ok in its condition is reported at its header, and an if that checks a variable against
// nil around a sole inner if that asserts on it and tests ok alone is reported at the inner if's header. No if matches
// both, since the first needs an initializer and the second refuses one.
func Check(context *rule.Context, ifStatement *ast.IfStmt) {
	checkConditionBesideAssertion(context, ifStatement)
	checkConditionAroundAssertion(context, ifStatement)
}

// checkConditionBesideAssertion is the first query: the initializer asserts on a variable named by an identifier,
// binding `assert`, and the condition is ok and that variable's nil check joined by `&&`, in either order.
func checkConditionBesideAssertion(context *rule.Context, ifStatement *ast.IfStmt) {
	assertion, okObject, isAssertion := assertionInto(context, ifStatement.Init)
	if !isAssertion {
		return
	}
	// `assert@(Object _)` binds the object of an identifier alone, parenthesized or not: a selector is never one.
	asserted, isIdentifier := ast.Unparen(assertion.X).(*ast.Ident)
	if !isIdentifier {
		return
	}
	// A nil object is one the original's report would fail to assert as an object, and one no identifier of a package
	// that type-checks has.
	assertedObject := context.TypesInfo.ObjectOf(asserted)
	if assertedObject == nil {
		return
	}
	// `(BinaryExpr ...)` sees through parentheses around the whole condition as around each operand.
	condition, isBinary := ast.Unparen(ifStatement.Cond).(*ast.BinaryExpr)
	if !isBinary || condition.Op != token.LAND {
		return
	}
	// Each alternative of the `Or` is ordered: ok first and the nil check second, or the nil check first and ok
	// second. Either side alone, or a nil check spelled `nil != x`, is neither.
	okFirst := namesObject(context, condition.X, okObject) && isNilCheckOf(context, condition.Y, assertedObject)
	okSecond := isNilCheckOf(context, condition.X, assertedObject) && namesObject(context, condition.Y, okObject)
	if !okFirst && !okSecond {
		return
	}
	// The body and the else are `_ _`, so any body and any else, or none, are the same here.
	staticcheck_report.Report(context, ifStatement,
		fmt.Sprintf("when %s is true, %s can't be nil", okObject.Name(), assertedObject.Name()),
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated())
}

// checkConditionAroundAssertion is the second query: an if of no initializer and no else checks a variable named by
// an identifier against nil, binding `lhs`, and its body is a sole inner if of no else that asserts on that variable
// and tests ok alone. The finding is at the inner if, `ifstmt`.
func checkConditionAroundAssertion(context *rule.Context, ifStatement *ast.IfStmt) {
	// The query's first nil is no initializer: the matcher's nil matches an absent statement alone.
	if ifStatement.Init != nil || !isAbsentElse(ifStatement.Else) {
		return
	}
	condition, isBinary := ast.Unparen(ifStatement.Cond).(*ast.BinaryExpr)
	if !isBinary || condition.Op != token.NEQ || !isBuiltinNil(context, condition.Y) {
		return
	}
	// `lhs@(Object _)` binds the object of an identifier alone, parenthesized or not, here on the left of `!=`.
	checked, isIdentifier := ast.Unparen(condition.X).(*ast.Ident)
	if !isIdentifier {
		return
	}
	// A nil object is one the original's report would fail to assert as an object, and one no identifier of a package
	// that type-checks has.
	checkedObject := context.TypesInfo.ObjectOf(checked)
	if checkedObject == nil {
		return
	}
	inner, isIf := soleStatement(ifStatement.Body).(*ast.IfStmt)
	if !isIf || !isAbsentElse(inner.Else) {
		return
	}
	assertion, okObject, isAssertion := assertionInto(context, inner.Init)
	if !isAssertion {
		return
	}
	// `(TypeAssertExpr lhs _)` recalls the bound object, which an identifier or a selector naming it matches, so the
	// inner if may assert on the same variable through a dot import's other spelling.
	if !namesObject(context, assertion.X, checkedObject) {
		return
	}
	// The inner condition is the bare `ok`, the recalled object alone, parenthesized or not.
	if !namesObject(context, inner.Cond, okObject) {
		return
	}
	// The inner body is `_`, so any body is the same here.
	staticcheck_report.Report(context, inner,
		fmt.Sprintf("when %s is true, %s can't be nil", okObject.Name(), checkedObject.Name()),
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated())
}

// assertionInto is the type assertion an initializer matches `(AssignStmt [(Ident "_") ok@(Object _)] _
// [(TypeAssertExpr ...)])` with, and the object it binds as ok, or false. The statement is an assignment by `:=` or
// `=`, as `_` takes any token, of exactly two left sides, the blank identifier and an identifier with an object, and
// exactly one right side, a type assertion, each parenthesized or not.
func assertionInto(context *rule.Context, initializer ast.Stmt) (*ast.TypeAssertExpr, types.Object, bool) {
	// A missing initializer matches no assignment pattern, and the matcher unwraps no statement an initializer can be
	// into an assignment.
	assignment, isAssignment := initializer.(*ast.AssignStmt)
	if !isAssignment || len(assignment.Lhs) != 2 || len(assignment.Rhs) != 1 {
		return nil, nil, false
	}
	// `(Ident "_")` compares the name alone, and only the blank identifier is spelled `_`.
	blank, isBlank := ast.Unparen(assignment.Lhs[0]).(*ast.Ident)
	if !isBlank || blank.Name != "_" {
		return nil, nil, false
	}
	okIdentifier, isIdentifier := ast.Unparen(assignment.Lhs[1]).(*ast.Ident)
	if !isIdentifier {
		return nil, nil, false
	}
	// A nil object, the blank identifier's in `_, _ = x.(T)`, is one no condition of a package that type-checks
	// recalls, and one the original's report would fail to assert as an object.
	okObject := context.TypesInfo.ObjectOf(okIdentifier)
	if okObject == nil {
		return nil, nil, false
	}
	assertion, isAssertion := ast.Unparen(assignment.Rhs[0]).(*ast.TypeAssertExpr)
	if !isAssertion {
		return nil, nil, false
	}
	return assertion, okObject, true
}

// isNilCheckOf is whether expression matches `(BinaryExpr assert "!=" (Builtin "nil"))` with assert recalled as
// object: a comparison by `!=` of what names object, on the left, with the universe's nil, on the right, through any
// parentheses.
func isNilCheckOf(context *rule.Context, expression ast.Expr, object types.Object) bool {
	comparison, isBinary := ast.Unparen(expression).(*ast.BinaryExpr)
	if !isBinary || comparison.Op != token.NEQ {
		return false
	}
	return namesObject(context, comparison.X, object) && isBuiltinNil(context, comparison.Y)
}

// isBuiltinNil is whether expression is the predeclared nil, as the pattern's `(Builtin "nil")` matches it: an
// identifier spelled nil, parenthesized or not, whose object is the universe's, so a nil the code declares itself, in
// a package or a function, is not it.
func isBuiltinNil(context *rule.Context, expression ast.Expr) bool {
	identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	if !isIdentifier || identifier.Name != "nil" {
		return false
	}
	return context.TypesInfo.ObjectOf(identifier) == types.Universe.Lookup("nil")
}

// namesObject is whether expression names object, as the pattern recalls the object a binding holds: an identifier
// whose object it is, or a selector whose selected name's object it is, parenthesized or not. The objects are compared
// by identity, so two variables spelled alike are two, and a package's variable named through a dot import and
// through its package name is one.
func namesObject(context *rule.Context, expression ast.Expr, object types.Object) bool {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.Ident:
		return context.TypesInfo.ObjectOf(expression) == object
	case *ast.SelectorExpr:
		return context.TypesInfo.ObjectOf(expression.Sel) == object
	default:
		return false
	}
}

// isAbsentElse is whether elseStatement matches the pattern's nil: no else, or an else block of no statements. The
// matcher turns a block into its list and takes a nil list for nil, and the parser leaves the list of a block it reads
// no statement in nil, so `else {}`, and an else holding only a comment, match as no else at all. An else if, or an
// else holding an empty block, is a statement, and is not.
func isAbsentElse(elseStatement ast.Stmt) bool {
	if elseStatement == nil {
		return true
	}
	block, isBlock := elseStatement.(*ast.BlockStmt)
	return isBlock && block.List == nil
}

// soleStatement is the one statement block holds, as the pattern matches a block against a list of one: by its
// statements, of which there must be exactly one. The matcher unwraps a labeled statement to the statement it labels
// and a block to its statements wherever either stands, so the sole statement is seen through any labels on it and
// any blocks of one statement around it, and the if bound is the if itself, never its label. A block of none or of
// more than one statement, at any depth, holds no sole statement, and the result is nil.
func soleStatement(block *ast.BlockStmt) ast.Stmt {
	if len(block.List) != 1 {
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
			// The matcher unwraps an expression statement and a declaration statement too, to what is never an if.
			return statement
		}
	}
}
