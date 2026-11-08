// Package unnecessary_blank_identifier is staticcheck's S1005, an assignment to the blank identifier that says
// nothing, `_ = <-ch`, `x, _ = <-ch`, `for _ = range s` and `for x, _ = range s`, each the same statement written
// longer, ported as a check on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1005/s1005.go, whose logic it keeps and whose
// messages and fixes it reports word for word, at the same ranges: the assignment, or the range statement's key or
// value. The original matches its assignments with two queries, `(AssignStmt [_ (Ident "_")] _ (UnaryExpr "<-" _))`
// and `(AssignStmt (Ident "_") _ recv@(UnaryExpr "<-" _))`, through staticcheck's pattern language, and judges its
// range statements by syntax already; the port asks the same of each assignment directly, reading pattern/match.go for
// what each construct of the queries matches, and reports through staticcheck_report.Report where the original reports
// through report.Report, behind the same gate: a range statement may leave out its variables from go1.4 on, so a file
// whose language is older is told nothing of one. The registry still runs the original, which stays the oracle: the
// parity tests beside this file run both over the same packages, under go1.0, go1.3 and go1.4 alike, and the two
// differ nowhere they hold.
package unnecessary_blank_identifier

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/go/ast/astutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, since this check filters them.
var Analyzer = &analysis.Analyzer{
	Name:     "S1005",
	Doc:      "Flags an assignment to the blank identifier that can be left out: staticcheck's S1005, ported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// message is the original's, in every branch.
const message = "unnecessary assignment to the blank identifier"

// run builds the pass's context and judges every assignment of the package, then every range statement, in the
// original's order.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("unnecessary_blank_identifier: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.AssignStmt)(nil)}, func(node ast.Node) {
		assignment, isAssignment := node.(*ast.AssignStmt)
		if !isAssignment {
			// Invariant: Preorder hands this function only the assignments it was asked for.
			panic("unnecessary_blank_identifier: the inspector handed over a node that is not an assignment")
		}
		Check(context, assignment)
	})
	inspected.Preorder([]ast.Node{(*ast.RangeStmt)(nil)}, func(node ast.Node) {
		rangeStatement, isRange := node.(*ast.RangeStmt)
		if !isRange {
			// Invariant: Preorder hands this function only the range statements it was asked for.
			panic("unnecessary_blank_identifier: the inspector handed over a node that is not a range statement")
		}
		CheckRange(context, rangeStatement)
	})
	return nil, nil
}

// Check judges one assignment: a channel receive assigned to the blank identifier alone, or to a second left side
// that is the blank identifier, receives what it would without the blank. A comma-ok receive into one variable and
// the blank, `x, _ = <-ch`, fixes to the receive into the one variable, and `_ = <-ch` to the receive alone. A map
// index, `x, _ = m[k]`, is left alone, as the original leaves it: the blank can say that the entry may be missing.
func Check(context *rule.Context, assignment *ast.AssignStmt) {
	// Both queries end in `(UnaryExpr "<-" _)`, a lone node against the right sides, which matches a list of one,
	// looked through its parentheses, and whose operator is the receive. The token between the sides is `_`, so a
	// definition, `x, _ := <-ch`, matches as an assignment does.
	if len(assignment.Rhs) != 1 {
		return
	}
	receive, isUnary := ast.Unparen(assignment.Rhs[0]).(*ast.UnaryExpr)
	if !isUnary || receive.Op != token.ARROW {
		return
	}
	switch len(assignment.Lhs) {
	case 2:
		// `[_ (Ident "_")]` is a list of exactly two, the first anything at all, a blank among them, and the second an
		// identifier spelled _, looked through its parentheses as the pattern looks through them everywhere.
		if !astutil.IsBlank(ast.Unparen(assignment.Lhs[1])) {
			return
		}
		// The fix prints a shallow copy of the assignment with its first left side alone, as the original's
		// `r := *node` does: the copy keeps the original's positions, so the printer breaks its lines where the
		// source does, and prints none of its comments, which a node outside its file carries none of.
		shortened := *assignment
		shortened.Lhs = shortened.Lhs[0:1]
		staticcheck_report.Report(context, assignment, message,
			staticcheck_report.FilterGenerated(),
			staticcheck_report.Fixes(edit.Fix("Remove assignment to blank identifier",
				edit.ReplaceWithNode(context.Fset, assignment, &shortened))))
	case 1:
		// `(Ident "_")` is a lone node against the left sides, so it matches a list of one, looked through its
		// parentheses.
		if !astutil.IsBlank(ast.Unparen(assignment.Lhs[0])) {
			return
		}
		// `recv@` binds the receive the match reached, inside its parentheses, so the fix prints the receive bare.
		staticcheck_report.Report(context, assignment, message,
			staticcheck_report.FilterGenerated(),
			staticcheck_report.Fixes(edit.Fix("Simplify channel receive operation",
				edit.ReplaceWithNode(context.Fset, assignment, receive))))
	}
}

// CheckRange judges one range statement: a blank key with no value, `for _ = range s`, a blank key and value, `for _,
// _ = range s`, or a blank value after a key, `for x, _ = range s`, each says what leaving the blank out says, from
// go1.4 on. The fixes delete by position, as the original's do, so their text is the source's own. The blanks are
// judged as written, as the original's astutil.IsBlank judges them, so a parenthesized one, `for (_) = range s`, is
// not one.
func CheckRange(context *rule.Context, rangeStatement *ast.RangeStmt) {
	ranged := context.TypesInfo.TypeOf(rangeStatement.X)
	if ranged == nil {
		// A ranged expression of no recorded type is one no package that type-checks holds, and both sides judge
		// only those; the original would crash on it, and the port reports nothing.
		return
	}
	// A range over a function names its iteration variables by their count, so they are not optional there. The
	// original asks the ranged type's underlying type, so a type parameter, whose underlying type is its constraint,
	// is not a function here even where its core type is one.
	if _, isSignature := ranged.Underlying().(*types.Signature); isSignature {
		return
	}

	// `for _ = range s`: the key and the token after it are deleted, through the token's first byte, as the
	// original's range does.
	if rangeStatement.Value == nil && astutil.IsBlank(rangeStatement.Key) {
		staticcheck_report.Report(context, rangeStatement.Key, message,
			staticcheck_report.FilterGenerated(),
			staticcheck_report.MinimumLanguageVersion("go1.4"),
			staticcheck_report.Fixes(edit.Fix("Remove assignment to blank identifier",
				edit.Delete(edit.Range{rangeStatement.Key.Pos(), rangeStatement.TokPos + 1}))))
	}

	// `for _, _ = range s`: reported at the key alone, as the original reports it, and fixed as the first.
	if astutil.IsBlank(rangeStatement.Key) && astutil.IsBlank(rangeStatement.Value) {
		staticcheck_report.Report(context, rangeStatement.Key, message,
			staticcheck_report.FilterGenerated(),
			staticcheck_report.MinimumLanguageVersion("go1.4"),
			staticcheck_report.Fixes(edit.Fix("Remove assignment to blank identifier",
				edit.Delete(edit.Range{rangeStatement.Key.Pos(), rangeStatement.TokPos + 1}))))
	}

	// `for x, _ = range s`: reported at the value, and everything from the key's end to the value's is deleted.
	if !astutil.IsBlank(rangeStatement.Key) && astutil.IsBlank(rangeStatement.Value) {
		staticcheck_report.Report(context, rangeStatement.Value, message,
			staticcheck_report.FilterGenerated(),
			staticcheck_report.MinimumLanguageVersion("go1.4"),
			staticcheck_report.Fixes(edit.Fix("Remove assignment to blank identifier",
				edit.Delete(edit.Range{rangeStatement.Key.End(), rangeStatement.Value.End()}))))
	}
}
