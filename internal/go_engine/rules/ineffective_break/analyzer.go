// Package ineffective_break is staticcheck's SA4011, a break that ends a switch or select case directly inside a loop,
// which leaves only the switch or select, where the loop was likely meant, ported as a check on syntax that reports
// through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4011/sa4011.go, whose logic it keeps and
// whose message it reports word for word, at the break's Pos and End, through staticcheck_report.Report, the port of
// the report.Report the original calls, proven against it beside it. It keeps the original's reach as well: a switch
// or select that is a statement of the loop's own body, not a type switch and not one nested deeper, and a break that
// is a case's last statement, or the last of an if that ends the case, and of its else block, one level deep. The
// original keeps no state from one loop to the next. The registry still runs the original, which stays the oracle:
// the parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package ineffective_break

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA4011",
	Doc: "Flags a break that ends a switch or select case inside a loop, which leaves the switch or select and not " +
		"the loop: staticcheck's SA4011, ported as a check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges the body of every for and range loop of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("ineffective_break: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.ForStmt)(nil), (*ast.RangeStmt)(nil)}, func(node ast.Node) {
		switch loop := node.(type) {
		case *ast.ForStmt:
			Check(context, loop.Body)
		case *ast.RangeStmt:
			Check(context, loop.Body)
		default:
			// Invariant: Preorder hands this function only the for and range statements it was asked for.
			panic("ineffective_break: the inspector handed over a node that is not a loop")
		}
	})
	return nil, nil
}

// Check judges the body of one loop: each switch or select among its statements has the break that ends any of its
// cases reported.
func Check(context *rule.Context, body *ast.BlockStmt) {
	for _, statement := range body.List {
		cases := [][]ast.Stmt{}
		switch statement := statement.(type) {
		case *ast.SwitchStmt:
			for _, clause := range statement.Body.List {
				caseClause, isCaseClause := clause.(*ast.CaseClause)
				if !isCaseClause {
					// Invariant: the parser puts only case clauses in a switch's body.
					panic("ineffective_break: a switch's body holds a statement that is not a case clause")
				}
				cases = append(cases, caseClause.Body)
			}
		case *ast.SelectStmt:
			for _, clause := range statement.Body.List {
				communicationClause, isCommunicationClause := clause.(*ast.CommClause)
				if !isCommunicationClause {
					// Invariant: the parser puts only communication clauses in a select's body.
					panic("ineffective_break: a select's body holds a statement that is not a communication clause")
				}
				cases = append(cases, communicationClause.Body)
			}
		default:
			continue
		}
		for _, caseBody := range cases {
			for _, last := range lastStatements(caseBody) {
				branch, isBranch := last.(*ast.BranchStmt)
				// A labeled break names what it leaves, so it says what it means.
				if !isBranch || branch.Tok != token.BREAK || branch.Label != nil {
					continue
				}
				staticcheck_report.Report(context, branch,
					"ineffective break statement. Did you mean to break out of the outer loop?")
			}
		}
	}
}

// lastStatements are the statements a case body ends on: its last statement, or, when that is an if, the last of
// the if's body and the last of its else block, one level deep and no deeper, as the original reads them. An if whose
// body is empty ends the case on nothing, its else block included.
func lastStatements(caseBody []ast.Stmt) []ast.Stmt {
	if len(caseBody) == 0 {
		return nil
	}
	last := caseBody[len(caseBody)-1]
	ifStatement, isIf := last.(*ast.IfStmt)
	if !isIf {
		return []ast.Stmt{last}
	}
	if len(ifStatement.Body.List) == 0 {
		return nil
	}
	lasts := []ast.Stmt{ifStatement.Body.List[len(ifStatement.Body.List)-1]}
	// An else if is not an else block, so its breaks are not read.
	if elseBlock, isBlock := ifStatement.Else.(*ast.BlockStmt); isBlock && len(elseBlock.List) != 0 {
		lasts = append(lasts, elseBlock.List[len(elseBlock.List)-1])
	}
	return lasts
}
