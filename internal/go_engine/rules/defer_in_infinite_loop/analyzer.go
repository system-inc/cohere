// Package defer_in_infinite_loop is staticcheck's SA5003, a defer inside a loop that never ends, which never runs,
// since a defer waits for its function to return, ported as a check on syntax that reports through the kit every port
// of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5003/sa5003.go, whose logic it keeps and
// whose message it reports word for word, at each defer's Pos and End, through staticcheck_report.Report, the port of
// the report.Report the original calls, proven against it beside it. It keeps the original's approximation as well: a
// loop with no condition counts as never ending unless its body holds a return or a break, any break, though one in a
// switch, a select or a loop of its own leaves only that statement, and a goto or a call that never returns does not
// count. The original keeps no state from one loop to the next. The registry still runs the original, which stays the
// oracle: the parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package defer_in_infinite_loop

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
	Name: "SA5003",
	Doc: "Flags a defer in a loop that never ends, which never runs, since a defer waits for its function to " +
		"return: staticcheck's SA5003, ported as a check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every for loop of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("defer_in_infinite_loop: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.ForStmt)(nil)}, func(node ast.Node) {
		loop, isLoop := node.(*ast.ForStmt)
		if !isLoop {
			// Invariant: Preorder hands this function only the for statements it was asked for.
			panic("defer_in_infinite_loop: the inspector handed over a node that is not a for statement")
		}
		Check(context, loop)
	})
	return nil, nil
}

// Check judges one for loop: one with no condition whose body holds no return and no break reports every defer in its
// body, a nested loop's among them, which then reports its own as well, as the original does.
func Check(context *rule.Context, loop *ast.ForStmt) {
	if loop.Cond != nil {
		return
	}
	mightExit := false
	defers := []*ast.DeferStmt{}
	ast.Inspect(loop.Body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.ReturnStmt:
			mightExit = true
			return false
		case *ast.BranchStmt:
			// Any break counts, even one that leaves only a switch, a select or an inner loop, so the original misses
			// some loops that never end, and the port with it.
			if statement.Tok == token.BREAK {
				mightExit = true
				return false
			}
		case *ast.DeferStmt:
			defers = append(defers, statement)
		case *ast.FuncLit:
			// A function literal's defers and returns are its own, not the loop's.
			return false
		}
		return true
	})
	if mightExit {
		return
	}
	for _, deferred := range defers {
		staticcheck_report.Report(context, deferred, "defers in this infinite loop will never run")
	}
}
