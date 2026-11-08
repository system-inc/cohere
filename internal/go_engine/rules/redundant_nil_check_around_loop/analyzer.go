// Package redundant_nil_check_around_loop is staticcheck's S1031, an if statement whose only work is a range over a
// slice or a map it first checks is not nil, `if s != nil { for range s {} }`, where the range over a nil slice or map
// runs zero times anyway, ported as a check on types and syntax that reports through the kit every port of
// staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1031/s1031.go, whose logic it keeps and whose
// message it reports word for word, at the same range, the if keyword through its condition. The original matches
// its one query, `(IfStmt nil (BinaryExpr x@(Object _) "!=" (Builtin "nil")) [(RangeStmt _ _ _ x _)] nil)`, through
// staticcheck's pattern language, then asks that every type the checked variable may be is a slice or a map; the
// port asks the same of each if statement directly, reading pattern/match.go for what each construct of the query
// matches, and reports through staticcheck_report.Report where the original reports through report.Report. The
// registry still runs the original, which stays the oracle: the parity tests beside this file run both over the same
// packages, and the two differ nowhere they hold.
package redundant_nil_check_around_loop

import (
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every if statement, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.IfStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		statement, isIf := node.(*ast.IfStmt)
		if !isIf {
			// Invariant: the dispatcher hands Node only the if statements it was asked for.
			panic("redundant_nil_check_around_loop: the dispatcher handed over a node that is not an if statement")
		}
		Check(context, statement)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1031",
	"S1031",
	"Flags a nil check around a range over a slice or a map: staticcheck's S1031, ported.",
	&Listener,
)

// Check judges one if statement: one with no init statement and no else, whose condition says a variable, named by
// an identifier, is not the predeclared nil, and whose body is a range over that very variable and nothing else,
// checks what the range already knows when every type the variable may be is a slice or a map.
func Check(context *rule.Context, statement *ast.IfStmt) {
	// The query's first nil is the init statement, which must be absent, so `if s := f(); s != nil` is left alone.
	if statement.Init != nil {
		return
	}
	// The query's last nil is the else, which an else if or an else with statements is not. An empty else block is,
	// since the pattern turns a block into its list and the parser leaves an empty block's list nil.
	if statement.Else != nil {
		elseBlock, isBlock := statement.Else.(*ast.BlockStmt)
		if !isBlock || elseBlock.List != nil {
			return
		}
	}
	// The pattern sees through parentheses around the condition and around each operand, and a BinaryExpr is an
	// ordered shape, so `nil != s` is not the query's comparison.
	comparison, isComparison := ast.Unparen(statement.Cond).(*ast.BinaryExpr)
	if !isComparison || comparison.Op != token.NEQ {
		return
	}
	// `(Object _)` matches an identifier alone, parenthesized or not, so a selector such as `t.values != nil` is
	// never one, even where its type is a slice.
	checked, isIdentifier := ast.Unparen(comparison.X).(*ast.Ident)
	if !isIdentifier {
		return
	}
	if !staticcheck_code.IsBuiltin(context, comparison.Y, "nil") {
		return
	}
	// The binding holds the identifier's object, and a nil object is one the pattern's second x never equals.
	checkedObject := context.TypesInfo.ObjectOf(checked)
	if checkedObject == nil {
		return
	}
	loop, isRange := soleRange(statement.Body.List)
	if !isRange || !namesObject(context, loop.X, checkedObject) {
		return
	}
	// The range's key, value, token and body are each `_` in the query, so any of them, or none, is the same here.
	if !typeutil.All(checkedObject.Type(), rangesZeroTimesWhenNil) {
		return
	}
	staticcheck_report.Report(context, statement, "unnecessary nil check around range",
		staticcheck_report.ShortRange(), staticcheck_report.FilterGenerated())
}

// soleRange is the range statement a list of one statement holds, looking through labels and blocks of one statement
// on the way, as match.go looks through them before comparing a statement with the pattern's RangeStmt, the list in
// brackets meaning exactly one. A list of any other length, an empty block among them, holds none.
func soleRange(statements []ast.Stmt) (*ast.RangeStmt, bool) {
	if len(statements) != 1 {
		return nil, false
	}
	statement := statements[0]
	for {
		switch wrapped := statement.(type) {
		case *ast.RangeStmt:
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

// namesObject is whether ranged names object, as the pattern recalls the object a binding holds: an identifier whose
// object it is, or a selector whose selected name's object it is, parenthesized or not. The objects are compared by
// identity, so two variables spelled alike are two, and a package's variable named through a dot import and through
// its package name is one.
func namesObject(context *rule.Context, ranged ast.Expr, object types.Object) bool {
	switch ranged := ast.Unparen(ranged).(type) {
	case *ast.Ident:
		return context.TypesInfo.ObjectOf(ranged) == object
	case *ast.SelectorExpr:
		return context.TypesInfo.ObjectOf(ranged.Sel) == object
	default:
		return false
	}
}

// rangesZeroTimesWhenNil is whether a range over a nil value of the term's type runs zero times without blocking,
// which a slice and a map do. The original also names a channel, a pointer, a function and a type parameter as no,
// and panics through lint.ExhaustiveTypeSwitch on any other type, which nothing compared with nil and ranged over in a
// package that type-checks can be; the port answers no there instead of stopping the process.
func rangesZeroTimesWhenNil(term *types.Term) bool {
	switch term.Type().Underlying().(type) {
	case *types.Slice, *types.Map:
		return true
	default:
		return false
	}
}
