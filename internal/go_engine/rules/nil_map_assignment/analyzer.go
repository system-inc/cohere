// Package nil_map_assignment is staticcheck's SA5000, a write into a map that is certainly nil, which panics, ported
// onto the engine's own intermediate representation, the first reader to walk it (#g0rwdw1).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5000/sa5000.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the update's syntax, where staticcheck's report
// puts it. The original walks every source function of a package in one run, but keeps nothing from one function to
// the next: each map update is judged alone, by the one value its map flattens to. So the port judges one function at
// a time and finds what the original finds, and reads the map through Flatten, irutil.Flatten translated onto our
// representation, so a phi whose edges are the same nil constant is that constant to both, and a phi of two values is
// nothing to either. The registry runs
// the port in the original's place, under its name, and the original stays only as the oracle: the parity tests beside
// this file run both over the same packages, and they differ nowhere. The one place the port could differ is an update
// with no syntax, where staticcheck's report panics and the port reports nothing; staticcheck builds every map update
// from an assignment or a literal, so no such update reaches either.
package nil_map_assignment

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
	rule.Namespace+"SA5000",
	"SA5000",
	"Flags an assignment into a map that is certainly nil: staticcheck's SA5000, ported onto the engine's own "+
		"intermediate representation.",
	&Listener,
)

// message is the original's, word for word.
const message = "assignment to nil map"

// Check judges one function: each map update whose map is, through every phi it passes, one nil constant.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			update, isUpdate := instruction.(*intermediate_representation.MapUpdate)
			if !isUpdate {
				continue
			}
			// Flatten is nil when the phis bring two different values, which no assertion holds.
			constant, isConstant := intermediate_representation.Flatten(
				update.Map,
			).(*intermediate_representation.Constant)
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
