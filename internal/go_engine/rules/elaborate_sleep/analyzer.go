// Package elaborate_sleep is staticcheck's S1037, a select statement whose one case receives from time.After,
// `select { case <-time.After(d): }`, which is `time.Sleep(d)` written longer, ported as a check on types and syntax
// that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1037/s1037.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the select keyword. The original matches its one
// query, `(SelectStmt (CommClause (UnaryExpr "<-" (CallExpr (Symbol "time.After") [arg])) body))`, through
// staticcheck's pattern language, and builds its fix, for a case with an empty body, from a second, `(CallExpr
// (SelectorExpr (Ident "time") (Ident "Sleep")) [arg])`, through edit.ReplaceWithPattern. The port asks the same of
// each select statement directly, reading pattern/match.go for what each construct of the query matches, builds the
// fix's syntax itself as pattern.NodeToAST builds it, around the argument as bound, and reports through
// staticcheck_report.Report where the original reports through report.Report. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ only where the original's fix names the package time by a name the
// file does not give it, each case recorded in the differences fixture: the port moves the fix to the name the file
// does give it, and withholds the fix where no name it could write calls time.Sleep.
package elaborate_sleep

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every select statement, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.SelectStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		selectStatement, isSelect := node.(*ast.SelectStmt)
		if !isSelect {
			// Invariant: the dispatcher hands Node only the select statements it was asked for.
			panic("elaborate_sleep: the dispatcher handed over a node that is not a select statement")
		}
		Check(context, selectStatement)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1037",
	"S1037",
	"Flags a select whose one case receives from time.After, in place of time.Sleep: "+
		"staticcheck's S1037, ported.",
	&Listener,
)

// message is the original's, in both of its branches.
const message = "should use time.Sleep instead of elaborate way of sleeping"

// Check judges one select statement: a select of one case, which receives from a call of time.After on one argument
// and keeps nothing it receives, waits as time.Sleep does, and where the case's body is empty, the fix calls
// time.Sleep on the argument in the select's place.
func Check(context *rule.Context, selectStatement *ast.SelectStmt) {
	// code.Matches asks the type index whether it holds time.After before it walks a package, and the index keeps the
	// packages a package imports, never the package itself, so the time package's own selects are never walked.
	if context.Package.Path() == "time" {
		return
	}
	// The query's one CommClause against the select's body, which match.go turns into its list, is a list of one: a
	// second case, a default among them, or none at all, is not the query's shape.
	if len(selectStatement.Body.List) != 1 {
		return
	}
	clause, isClause := selectStatement.Body.List[0].(*ast.CommClause)
	if !isClause {
		// Invariant: the parser fills a select's body with communication clauses alone.
		panic("elaborate_sleep: a select statement holds a statement that is not a communication clause")
	}
	after, isAfter := receivedAfter(context, clause.Comm)
	if !isAfter {
		return
	}

	// `body` binds the clause's statements as the parser listed them, so a body of one empty block, or of a lone
	// semicolon the parser keeps as an empty statement, is no empty body, and the original reports it with no fix.
	if len(clause.Body) != 0 {
		staticcheck_report.Report(context, selectStatement, message,
			staticcheck_report.ShortRange(),
			staticcheck_report.FilterGenerated())
		return
	}
	fixes := []analysis.SuggestedFix{}
	if sleep, isSpelled := sleepFunction(context, selectStatement, after.Fun); isSpelled {
		// `arg` binds the argument without the parentheses the pattern looks through, `time.After((d))`, and the fix's
		// call holds that node itself, not a copy, as pattern.NodeToAST places a binding's value, so format prints it
		// from its own positions.
		sleepCall := &ast.CallExpr{Fun: sleep, Args: []ast.Expr{ast.Unparen(after.Args[0])}}
		fixes = append(fixes,
			edit.Fix("Use time.Sleep", edit.ReplaceWithNode(context.FileSet, selectStatement, sleepCall)))
	}
	staticcheck_report.Report(context, selectStatement, message,
		staticcheck_report.ShortRange(),
		staticcheck_report.FilterGenerated(),
		staticcheck_report.Fixes(fixes...))
}

// receivedAfter is the call of time.After a clause's communication receives from, as the query's `(UnaryExpr "<-"
// (CallExpr (Symbol "time.After") [arg]))` matches it: an expression statement, the receive parenthesized or not,
// `case (<-time.After(d)):`, of a call parenthesized or not, of one argument, whose callee is time.After, as
// staticcheck_code.IsSymbol names it, so a dot import and a renamed import name it, and a function of the code's own
// or a variable holding time.After does not. A receive whose value is assigned, `case t := <-time.After(d):`, is an
// assignment, a default case has no communication, and a send is a send statement, so none is the query's shape.
func receivedAfter(context *rule.Context, communication ast.Stmt) (*ast.CallExpr, bool) {
	statement, isExpression := communication.(*ast.ExprStmt)
	if !isExpression {
		return nil, false
	}
	receive, isUnary := ast.Unparen(statement.X).(*ast.UnaryExpr)
	if !isUnary || receive.Op != token.ARROW {
		return nil, false
	}
	call, isCall := ast.Unparen(receive.X).(*ast.CallExpr)
	if !isCall || len(call.Args) != 1 || !staticcheck_code.IsSymbol(context, call.Fun, "time.After") {
		return nil, false
	}
	return call, true
}

// sleepFunction is how the fix names time.Sleep where the select stands, and whether any name does. The original
// writes `time.Sleep` always, the pattern's `(Ident "time")`, which fails to compile, or calls something else, where
// time names no import of the package time there: a file that imports it under another name or into its own scope,
// or a scope that declares a time of its own. The port writes `time.Sleep` wherever time names that import, which is
// the original's fix to the byte, and elsewhere moves the fix to the name the code itself calls time.After by, which
// stands in the same scope as the select: the package name of a selector, `clock.Sleep`, or for a dot import Sleep
// alone, where Sleep is time's. Where neither names it, a dot import whose Sleep the code declares a Sleep over, the
// port keeps the finding and withholds the fix. Each case is recorded in the differences fixture.
func sleepFunction(context *rule.Context, selectStatement *ast.SelectStmt, afterFunction ast.Expr) (ast.Expr, bool) {
	scope := context.Package.Scope().Innermost(selectStatement.Pos())
	if scope == nil {
		// Invariant: a select statement stands in a function's body, whose scope the type checker records around it.
		panic("elaborate_sleep: no scope holds a select statement")
	}
	// The select's position counts only what is declared before it in its own block, as the select's place does.
	if _, named := scope.LookupParent("time", selectStatement.Pos()); importsTime(named) {
		return edit.Selector("time", "Sleep"), true
	}
	switch named := ast.Unparen(afterFunction).(type) {
	case *ast.SelectorExpr:
		// A selector that names a package's function selects it from the package's name, an identifier.
		qualifier, isIdentifier := named.X.(*ast.Ident)
		if isIdentifier && importsTime(context.TypesInfo.Uses[qualifier]) {
			return edit.Selector(qualifier.Name, "Sleep"), true
		}
	case *ast.Ident:
		// An identifier alone names time.After only through a dot import, which brings time.Sleep into the file's
		// scope as Sleep, unless a scope nearer the select declares a Sleep of its own.
		after := context.TypesInfo.Uses[named]
		if after == nil || after.Pkg() == nil {
			return nil, false
		}
		_, sleep := scope.LookupParent("Sleep", selectStatement.Pos())
		if sleep != nil && sleep == context.LookupIn(after.Pkg(), "Sleep") {
			return &ast.Ident{Name: "Sleep"}, true
		}
	}
	return nil, false
}

// importsTime is whether object is the name of an import of the standard library's package time.
func importsTime(object types.Object) bool {
	packageName, isPackageName := object.(*types.PkgName)
	return isPackageName && packageName.Imported().Path() == "time"
}
