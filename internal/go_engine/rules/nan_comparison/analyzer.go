// Package nan_comparison is staticcheck's SA4012, a binary operation with math.NaN() on either side, as in a
// comparison that can never hold since no value equals NaN, ported onto the engine's own intermediate representation.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4012/sa4012.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the operation's syntax, where staticcheck's
// report puts it. The original walks every source function of a package in one run, but keeps nothing from one function
// to the next: each operation is judged by its own operands. So the port judges one function at a time and finds what
// the original finds. Like the original, it judges every binary operation, arithmetic as much as comparison, and sees
// through phis whose edges are all one call to math.NaN, through Flatten, irutil.Flatten translated onto our
// representation, and no further. The builder replaces a phi whose edges are one value with that value, so Flatten
// counts only where phis reach the call through one another, none of them trivial, as in the parity fixture's
// phisOfOneCall. The registry runs the port in the original's place, under its name, and the original stays only as the
// oracle: the parity tests beside this file run both over the same packages, and they differ nowhere. The one place the
// port could differ is an operation with no syntax, where staticcheck's report panics and the port reports nothing;
// staticcheck builds every binary operation from an expression, so no such operation reaches either.
package nan_comparison

import (
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher over the engine's own intermediate
// representation: every function, each judged on its own.
var Listener = rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check}}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4012",
	"SA4012",
	"Flags a comparison against math.NaN(), which no value equals: staticcheck's SA4012, ported onto the "+
		"engine's own intermediate representation.",
	&Listener,
)

// message is the original's, word for word.
const message = "no value is equal to NaN, not even NaN itself"

// Check judges one function: each binary operation one of whose operands is the result of math.NaN().
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			operation, isOperation := instruction.(*intermediate_representation.BinaryOperation)
			if !isOperation ||
				!isNaN(intermediate_representation.Flatten(operation.X)) &&
					!isNaN(intermediate_representation.Flatten(operation.Y)) {
				continue
			}
			source := operation.Source()
			if source == nil {
				// An operation with no syntax has no place to report; staticcheck's report panics on one.
				continue
			}
			context.Report(analysis.Diagnostic{Pos: source.Pos(), End: source.End(), Message: message})
		}
	}
}

// isNaN is whether value is the result of a call to math.NaN.
func isNaN(value intermediate_representation.Value) bool {
	call, isCall := value.(*intermediate_representation.Call)
	return isCall && intermediate_representation.IsCallTo(call.Common(), "math.NaN")
}
