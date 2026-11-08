// Package unchanging_loop_condition is staticcheck's SA4008, a three-clause for loop whose condition reads the variable
// its init declares and whose post statement changes something else, so the condition reads one value on every pass,
// ported onto staticcheck's intermediate representation as a rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4008/sa4008.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the loop's condition, where staticcheck's
// report puts it. The original walks every source function of a package in one run, but keeps nothing from one
// function to the next: it walks the syntax of each function and asks that same function what value the condition's
// variable holds. So the port judges one function at a time and finds what the original finds. The syntax of a
// function holds the syntax of the function literals inside it, which are functions of their own, so both the
// original and the port walk a loop inside a literal twice, once from each; the enclosing function holds no value for
// the literal's expressions, so only the literal judges the loop, and it is reported once. The registry runs the port
// in the original's place, under its name, and the original stays only as the oracle: the parity tests beside this file
// run both over the same packages, and they differ nowhere, since the port walks the same syntax and asks the same
// function the same question of it.
package unchanging_loop_condition

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4008",
	"SA4008",
	"Flags a for loop whose condition's variable never changes: staticcheck's SA4008, ported onto its intermediate "+
		"representation.",
	&Listener,
)

// message is the original's, word for word.
const message = "variable in loop condition never changes"

// Check judges one function: each for loop in its syntax that declares or assigns one variable in its init, compares
// that variable on the left of its condition, and increments or decrements in its post statement, reported when the
// value the condition reads is the same on every pass. A variable that changes from pass to pass is a phi at the loop's
// head, and one that lives in memory, its address taken or captured, is loaded afresh on every pass; any other value,
// a constant, a parameter, the result of a call, is the one the init gave it.
func Check(context *rule.Context, function *ir.Function) {
	source := function.Source()
	if source == nil {
		// A function with no syntax, a wrapper or a synthesized init, holds no loop to judge.
		return
	}
	ast.Inspect(source, func(node ast.Node) bool {
		loop, isLoop := node.(*ast.ForStmt)
		if !isLoop || loop.Init == nil || loop.Cond == nil || loop.Post == nil {
			return true
		}
		init, isAssign := loop.Init.(*ast.AssignStmt)
		if !isAssign || len(init.Lhs) != 1 || len(init.Rhs) != 1 {
			return true
		}
		condition, isBinary := loop.Cond.(*ast.BinaryExpr)
		if !isBinary {
			return true
		}
		// Only a bare name on the left of the condition, never one in parentheses, the original's reading.
		compared, isComparedName := condition.X.(*ast.Ident)
		if !isComparedName {
			return true
		}
		assigned, isAssignedName := init.Lhs[0].(*ast.Ident)
		if !isAssignedName {
			return true
		}
		if context.TypesInfo.ObjectOf(compared) != context.TypesInfo.ObjectOf(assigned) {
			return true
		}
		if _, isIncrement := loop.Post.(*ast.IncDecStmt); !isIncrement {
			return true
		}
		// The enclosing function of a function literal holds no value for the literal's expressions, so a loop
		// inside one is judged only by the literal itself.
		value, isAddress := function.ValueForExpr(condition.X)
		if value == nil || isAddress {
			return true
		}
		switch value.(type) {
		case *ir.Phi, *ir.Load:
			return true
		}
		context.Report(analysis.Diagnostic{Pos: condition.Pos(), End: condition.End(), Message: message})
		return true
	})
}
