// Package self_assignment is staticcheck's SA4018, an assignment of a variable or an expression to itself, which does
// nothing, ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4018/sa4018.go, whose logic it keeps and
// whose message it reports word for word, over the whole assignment, and in no generated file, as staticcheck's
// report.FilterGenerated leaves them out. It asks staticcheck_code.MayHaveSideEffects where the original asks
// code.MayHaveSideEffects, judging a function pure when staticcheck's purity pass holds it, and reports through
// staticcheck_report.Report where the original reports through report.Report, each the port of the one the original
// calls, proven against it beside it. It renders each side with staticcheck's own report.Render, given a pass that
// holds only the context's file set, which is all Render reads, so the two sides compare and print as the original's
// do. The registry still runs the original, which stays the oracle: the parity tests beside this file run both over
// the same packages, and the two differ nowhere they hold.
package self_assignment

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/purity"
	"honnef.co/go/tools/analysis/report"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher. The check judges syntax, every assignment of
// the package in the order the inspector walks them, once the walk is over, at PackageExit. It listens to the walk
// over the intermediate representation, not the syntax's, because it reads staticcheck's purity facts, which are
// built from that representation and so only on a package without errors, as the original runs: the syntax walk runs
// on every package, and so must everything a syntax listener requires.
var Listener = rule.Listener{
	Requires:    []*analysis.Analyzer{inspect.Analyzer, purity.Analyzer},
	Function:    hearFunction,
	PackageExit: judgeAssignments,
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4018",
	"SA4018",
	"Flags an assignment of a variable to itself: staticcheck's SA4018, ported as a check on syntax.",
	&Listener,
)

// hearFunction is the listener's Function, which a listener of the walk over the intermediate representation
// declares to hear it, and which keeps nothing of the function.
func hearFunction(*rule.Context, *ir.Function) {
	// Nothing to keep: the check judges the package's syntax once the walk is over, in judgeAssignments.
}

// judgeAssignments builds the purity judgment and judges every assignment of the package.
func judgeAssignments(context *rule.Context) {
	pure, isPurity := context.ResultOf(purity.Analyzer).(purity.Result)
	if !isPurity {
		// Invariant: the listener requires the purity pass, and the dispatcher hands over its result, of the type it
		// declares.
		panic("self_assignment: the purity pass's result is missing")
	}
	isPure := func(function *types.Func) bool {
		_, isHeld := pure[function]
		return isHeld
	}
	inspected, isInspector := context.ResultOf(inspect.Analyzer).(*inspector.Inspector)
	if !isInspector {
		// Invariant: the listener requires the inspect pass, and the dispatcher hands over its result, of the type it
		// declares.
		panic("self_assignment: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.AssignStmt)(nil)}, func(node ast.Node) {
		assignment, isAssignment := node.(*ast.AssignStmt)
		if !isAssignment {
			// Invariant: Preorder hands this function only the assignments it was asked for.
			panic("self_assignment: the inspector handed over a node that is not an assignment")
		}
		Check(context, assignment, isPure)
	})
}

// Check judges one assignment: each of its pairs whose two sides are the same kind of expression, have no side
// effects, and print the same, given isPure to judge which calls have none.
func Check(context *rule.Context, assignment *ast.AssignStmt, isPure func(function *types.Func) bool) {
	if assignment.Tok != token.ASSIGN || len(assignment.Lhs) != len(assignment.Rhs) {
		return
	}
	// Render reads nothing of a pass but its file set.
	renderPass := &analysis.Pass{Fset: context.Fset}
	for index, left := range assignment.Lhs {
		right := assignment.Rhs[index]
		// The sides must be the same kind of node, so `(x) = x` is left alone, though it assigns x to itself.
		if reflect.TypeOf(left) != reflect.TypeOf(right) {
			continue
		}
		if staticcheck_code.MayHaveSideEffects(context, left, isPure) ||
			staticcheck_code.MayHaveSideEffects(context, right, isPure) {
			continue
		}
		renderedLeft := report.Render(renderPass, left)
		renderedRight := report.Render(renderPass, right)
		if renderedLeft == renderedRight {
			staticcheck_report.Report(context, assignment,
				fmt.Sprintf("self-assignment of %s to %s", renderedRight, renderedLeft),
				staticcheck_report.FilterGenerated())
		}
	}
}
