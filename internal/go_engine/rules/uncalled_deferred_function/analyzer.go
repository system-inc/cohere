// Package uncalled_deferred_function is staticcheck's SA9010, a defer of a call that returns a function, where the
// function it returns was almost surely the one meant to be deferred, `defer f()` written for `defer f()()`, ported as
// a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa9010/sa9010.go, whose logic it keeps and
// whose message it reports word for word, at the defer's Pos and End, through staticcheck_report.Report, the port of
// the report.Report the original calls, proven against it beside it. A call returning a value of a named function
// type counts, since the type's underlying type is a signature, and a call returning several values does not, since
// its type is a tuple. The original keeps no state from one defer to the next. The registry still runs the original,
// which stays the oracle: the parity tests beside this file run both over the same packages, and the two differ
// nowhere they hold.
package uncalled_deferred_function

import (
	"go/ast"
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
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA9010",
	Doc: "Flags a defer of a call that returns a function, which defers the call and drops the function it " +
		"returns: staticcheck's SA9010, ported as a check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every defer of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("uncalled_deferred_function: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.DeferStmt)(nil)}, func(node ast.Node) {
		deferred, isDefer := node.(*ast.DeferStmt)
		if !isDefer {
			// Invariant: Preorder hands this function only the defer statements it was asked for.
			panic("uncalled_deferred_function: the inspector handed over a node that is not a defer statement")
		}
		Check(context, deferred)
	})
	return nil, nil
}

// Check judges one defer: one whose call returns a single function is reported.
func Check(context *rule.Context, deferred *ast.DeferStmt) {
	// The type checker records a type for every call of a package that type-checks, the empty tuple for a call that
	// returns nothing, so the type is never missing here, as the original assumes too.
	if _, isFunction := context.TypesInfo.TypeOf(deferred.Call).Underlying().(*types.Signature); isFunction {
		staticcheck_report.Report(context, deferred, "deferred return function not called")
	}
}
