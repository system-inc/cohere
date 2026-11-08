// Package nil_map_assignment is staticcheck's SA5000, a write into a map that is certainly nil, which panics, ported
// onto staticcheck's intermediate representation as a rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5000/sa5000.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the update's syntax, where staticcheck's report
// puts it. The original walks every source function of a package in one run, but keeps nothing from one function to
// the next: each map update is judged alone, by the one value its map flattens to. So the port judges one function at
// a time and finds what the original finds, and reads the map through the same irutil.Flatten, so a phi whose edges
// are the same nil constant is that constant to both, and a phi of two values is nothing to either. The original stays
// the oracle: the parity tests beside this file run both over the same packages, and they differ nowhere. The one
// place the port could differ is an update with no syntax, where staticcheck's report panics and the port reports
// nothing; staticcheck builds every map update from an assignment or a literal, so no such update reaches either.
package nil_map_assignment

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
	"SA5000",
	"Flags an assignment into a map that is certainly nil: staticcheck's SA5000, ported onto its intermediate "+
		"representation.",
	Check,
)

// message is the original's, word for word.
const message = "assignment to nil map"

// Check judges one function: each map update whose map is, through every phi it passes, one nil constant.
func Check(context *rule.Context, function *ir.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			update, isUpdate := instruction.(*ir.MapUpdate)
			if !isUpdate {
				continue
			}
			// Flatten is nil when the phis bring two different values, which no assertion holds.
			constant, isConstant := irutil.Flatten(update.Map).(*ir.Const)
			if !isConstant || constant.Value != nil {
				continue
			}
			source := update.Source()
			if source == nil {
				// An update with no syntax has no place to report; staticcheck's report panics on one.
				continue
			}
			context.Report(analysis.Diagnostic{Pos: source.Pos(), End: source.End(), Message: message})
		}
	}
}
