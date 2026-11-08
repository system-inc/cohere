// Package separate_declaration_and_assignment is staticcheck's S1021, a variable declared with no value and assigned
// its first value by the very next statement, which one declaration with that value says plainer, ported as a check
// on types and syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1021/s1021.go, whose logic it keeps and whose
// message and fix it reports word for word, at the same range, the declaration statement. The original uses no
// pattern: it walks every block's statements, and the port asks the same of each, by the same syntax and the same
// objects, and reports through staticcheck_report.Report where the original reports through report.Report. Where the
// original walks only blocks, the port walks the statements of a switch's and a select's clauses too, which are lists
// of statements as a block's are and which the original misses, since a clause is no block (the differences fixture
// records each). The registry still runs the original, which stays the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere else they hold.
package separate_declaration_and_assignment

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, since this check filters them.
var Analyzer = &analysis.Analyzer{
	Name:     "S1021",
	Doc:      "Flags a variable declaration whose next statement assigns it: staticcheck's S1021, ported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every block and every switch and select clause of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("separate_declaration_and_assignment: the inspect pass's result is missing")
	}
	inspected.Preorder(
		[]ast.Node{(*ast.BlockStmt)(nil), (*ast.CaseClause)(nil), (*ast.CommClause)(nil)},
		func(node ast.Node) {
			Check(context, node)
		},
	)
	return nil, nil
}

// Check judges one list of statements, a block's or a switch's or select's clause's: each variable declaration of one
// name and no value whose next statement assigns that variable alone, from a value that never names it, and which
// the list assigns nowhere else, is reported, with the fix that declares it with that value.
func Check(context *rule.Context, node ast.Node) {
	switch holder := node.(type) {
	case *ast.BlockStmt:
		checkStatements(context, holder, holder.List)
	case *ast.CaseClause:
		// The original walks blocks alone, and a clause's body is a list of statements of its own, no block, so it
		// never sees a declaration there; the port judges a clause's statements as it judges a block's.
		checkStatements(context, holder, holder.Body)
	case *ast.CommClause:
		checkStatements(context, holder, holder.Body)
	default:
		// Invariant: Preorder hands this function only the blocks and clauses it was asked for.
		panic("separate_declaration_and_assignment: the inspector handed over a node that holds no statements")
	}
}

// checkStatements judges each statement of statements but the last against the one after it, holder being the node
// that holds them, which is where every other assignment to the variable can be.
func checkStatements(context *rule.Context, holder ast.Node, statements []ast.Stmt) {
	for index := 0; index+1 < len(statements); index++ {
		declaration, isDeclaration := statements[index].(*ast.DeclStmt)
		if !isDeclaration {
			continue
		}
		general, isGeneral := declaration.Decl.(*ast.GenDecl)
		// One spec only, parenthesized or not, so the fix never drops another of the declaration's variables.
		if !isGeneral || general.Tok != token.VAR || len(general.Specs) != 1 {
			continue
		}
		specification, isValue := general.Specs[0].(*ast.ValueSpec)
		if !isValue || len(specification.Names) != 1 || len(specification.Values) != 0 {
			continue
		}
		assignment, isAssignment := statements[index+1].(*ast.AssignStmt)
		// Only a plain `=`: an operator assignment reads the variable's zero value, and a labeled assignment is a
		// labeled statement, no assignment, as the original sees it.
		if !isAssignment || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			continue
		}
		// The original asserts an identifier on the left without looking through parentheses, so `(x) = 1` is no
		// assignment to x here.
		assigned, isIdentifier := assignment.Lhs[0].(*ast.Ident)
		if !isIdentifier {
			continue
		}
		// Compared by object, so the blank identifier, whose declaration defines a variable and whose assignment
		// defines none, is never one, and a variable spelled alike in another scope is another.
		object := context.TypesInfo.ObjectOf(assigned)
		if context.TypesInfo.ObjectOf(specification.Names[0]) != object {
			continue
		}
		// A value that names the variable, in a function literal too, can't move into its declaration, where the
		// variable is not yet in scope.
		if staticcheck_code.RefersTo(context, assignment.Rhs[0], object) {
			continue
		}
		if isAssignedAgain(context, holder, object) {
			continue
		}
		merged := &ast.GenDecl{
			Specs: []ast.Spec{
				&ast.ValueSpec{
					Names:  specification.Names,
					Values: []ast.Expr{assignment.Rhs[0]},
					Type:   specification.Type,
				},
			},
			Tok: general.Tok,
		}
		staticcheck_report.Report(context, declaration,
			"should merge variable declaration with assignment on next line",
			staticcheck_report.FilterGenerated(),
			staticcheck_report.Fixes(edit.Fix("Merge declaration with assignment",
				edit.ReplaceWithNode(context.Fset, edit.Range{declaration.Pos(), assignment.End()}, merged))))
	}
}

// isAssignedAgain is whether holder, at any depth, function literals included, holds two assignments to object, the
// one being judged and another, as the original's hasMultipleAssignments counts them: every identifier on the left of
// an assignment of any token, `:=` and `+=` among them, whose object is object, and nothing else, so `x++`, a range
// clause and `(x) = 1` each count for nothing.
func isAssignedAgain(context *rule.Context, holder ast.Node, object types.Object) bool {
	count := 0
	ast.Inspect(holder, func(node ast.Node) bool {
		if count >= 2 {
			return false
		}
		assignment, isAssignment := node.(*ast.AssignStmt)
		if !isAssignment {
			return true
		}
		for _, left := range assignment.Lhs {
			if identifier, isIdentifier := left.(*ast.Ident); isIdentifier &&
				context.TypesInfo.ObjectOf(identifier) == object {
				count++
			}
		}
		return true
	})
	return count >= 2
}
