// Package invalid_utf8_cut_set is staticcheck's SA1011, a constant cutset or set of characters given to a function of
// the strings package that is not valid UTF-8, which those functions read as runes, ported onto staticcheck_call,
// the house's translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1011/sa1011.go, whose rules it keeps,
// one for each of the six functions, and whose message it reports word for word over the same range, the second
// argument's expression. The original keeps no state from one function to the next, nor does the port. The registry
// runs the port in the original's place, under its name, and the original stays only as the oracle: the parity tests
// beside this file run both over staticcheck's own fixture and the house's. The two differ nowhere: staticcheck_call
// departs from callcheck only on calls these functions are never part of, as testdata/src/differences records.
package invalid_utf8_cut_set

import (
	"go/constant"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to a function of the strings package that reads its second argument as a
// set of runes.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"strings.IndexAny":     checkCutset,
	"strings.LastIndexAny": checkCutset,
	"strings.ContainsAny":  checkCutset,
	"strings.Trim":         checkCutset,
	"strings.TrimLeft":     checkCutset,
	"strings.TrimRight":    checkCutset,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	FileLocal:                  true,
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1011",
	"SA1011",
	"Flags a constant cutset given to a function of the strings package that is not valid UTF-8: staticcheck's "+
		"SA1011, ported onto the engine's own intermediate representation.",
	&Listener,
)

// checkCutset marks the call's second argument when it is a constant string that is not valid UTF-8.
func checkCutset(call *staticcheck_call.Call) {
	cutset := call.Arguments[1]
	extracted := staticcheck_call.ExtractConstantOfKind(cutset.Value, constant.String)
	if extracted != nil && !utf8.ValidString(constant.StringVal(extracted.Value)) {
		cutset.Invalid("argument is not a valid UTF-8 encoded string")
	}
}
