// Package nan_comparison is staticcheck's SA4012, a binary operation with math.NaN() on either side, as in a
// comparison that can never hold since no value equals NaN, ported onto staticcheck's intermediate representation as a
// rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4012/sa4012.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the operation's syntax, where staticcheck's
// report puts it. The original walks every source function of a package in one run, but keeps nothing from one function
// to the next: each operation is judged by its own operands. So the port judges one function at a time and finds what
// the original finds. Like the original, it judges every binary operation, arithmetic as much as comparison, and sees
// through phis whose edges are all one call to math.NaN, through irutil.Flatten, and no further. The builder replaces a
// phi whose edges are one value with that value, so Flatten counts only where phis reach the call through one another,
// none of them trivial, as in the parity fixture's phisOfOneCall. The original stays the oracle: the parity tests
// beside this file run both over the same packages, and they differ nowhere. The one place the port could differ is an
// operation with no syntax, where staticcheck's report panics and the port reports nothing; staticcheck builds every
// binary operation from an expression, so no such operation reaches either.
package nan_comparison

import (
	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA4012",
	"Flags a comparison against math.NaN(), which no value equals: staticcheck's SA4012, ported onto its "+
		"intermediate representation.",
	Check,
)

// message is the original's, word for word.
const message = "no value is equal to NaN, not even NaN itself"

// Check judges one function: each binary operation one of whose operands is the result of math.NaN().
func Check(context *rule.Context, function *ir.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			operation, isOperation := instruction.(*ir.BinOp)
			if !isOperation || !isNaN(irutil.Flatten(operation.X)) && !isNaN(irutil.Flatten(operation.Y)) {
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
func isNaN(value ir.Value) bool {
	call, isCall := value.(*ir.Call)
	return isCall && irutil.IsCallTo(call.Common(), "math.NaN")
}
