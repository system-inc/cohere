// Package wait_group_add_in_goroutine is staticcheck's SA2000, (*sync.WaitGroup).Add called first thing inside the
// goroutine it counts, where it races the Wait it was meant to come before, ported as a check on syntax that reports
// through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa2000/sa2000.go, whose logic it keeps and
// whose message it reports word for word, over the same range, the call. The original matches the pattern
// `(GoStmt (CallExpr (FuncLit _ call@(CallExpr (Symbol "(*sync.WaitGroup).Add") _):_) _))` of staticcheck's pattern
// package; the port tests the same shape by its syntax: a go statement calling a function literal, parenthesized or
// not, whose first statement is a call to that method. The pattern reads that statement through the layers its match
// looks through, an expression statement, a label, parentheses, and a block of exactly one statement, and so does
// the port (firstCall). The method is judged by staticcheck_code.IsSymbol, the port of the pattern's Symbol, so a call
// through a method expression or a promoted method counts as the pattern counts it. The original first asks whether
// the package depends on sync at all, code.CouldMatchAny, and walks no further when it does not; any call the pattern
// matches resolves into sync, so the question changes no finding, and the port, asking nothing, walks every go
// statement. The message renders the call with staticcheck's report.Render, called with a pass that holds only the
// file set, which is all it reads; wave 5 ports it. It reports through staticcheck_report.Report with no option, so a
// generated file is judged like any other. The registry still runs the original, which stays the oracle: the parity
// tests beside this file run both over the same packages, and the two differ nowhere they hold.
package wait_group_add_in_goroutine

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/report"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA2000",
	Doc: "Flags (*sync.WaitGroup).Add called first inside the goroutine it counts, which races Wait: staticcheck's " +
		"SA2000, ported as a check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every go statement of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("wait_group_add_in_goroutine: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.GoStmt)(nil)}, func(node ast.Node) {
		statement, isGo := node.(*ast.GoStmt)
		if !isGo {
			// Invariant: Preorder hands this function only the go statements it was asked for.
			panic("wait_group_add_in_goroutine: the inspector handed over a node that is not a go statement")
		}
		Check(context, statement)
	})
	return nil, nil
}

// Check judges one go statement: one that starts a function literal whose first statement adds to a WaitGroup.
func Check(context *rule.Context, statement *ast.GoStmt) {
	literal, isLiteral := ast.Unparen(statement.Call.Fun).(*ast.FuncLit)
	if !isLiteral || len(literal.Body.List) == 0 {
		return
	}
	call := firstCall(literal.Body.List[0])
	if call == nil || !staticcheck_code.IsSymbol(context, call.Fun, "(*sync.WaitGroup).Add") {
		return
	}
	rendered := report.Render(&analysis.Pass{Fset: context.Fset}, call)
	staticcheck_report.Report(context, call,
		fmt.Sprintf("should call %s before starting the goroutine to avoid a race", rendered))
}

// firstCall is the call statement is, read through every layer the pattern's match looks through, or nil when it is
// none: an expression statement, a labeled statement, parentheses, and a block holding exactly one statement, which
// the match compares with the call as a list of one.
func firstCall(statement ast.Node) *ast.CallExpr {
	reached := statement
	for {
		switch layer := reached.(type) {
		case *ast.CallExpr:
			return layer
		case *ast.ExprStmt:
			reached = layer.X
		case *ast.ParenExpr:
			reached = layer.X
		case *ast.LabeledStmt:
			reached = layer.Stmt
		case *ast.BlockStmt:
			if len(layer.List) != 1 {
				return nil
			}
			reached = layer.List[0]
		default:
			return nil
		}
	}
}
