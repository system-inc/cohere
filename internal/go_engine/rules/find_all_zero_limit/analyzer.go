// Package find_all_zero_limit is staticcheck's SA1010, a FindAll method of *regexp.Regexp called with n == 0, which
// always returns no results, ported onto staticcheck_call, the house's translation of the callcheck framework the
// original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1010/sa1010.go, whose rules it keeps,
// one for each of the eight FindAll methods, and whose message it reports word for word over the same range, the
// limit argument's expression. The original keeps no state from one function to the next, nor does the port. Its
// rule is the original's exported RepeatZeroTimes, which SA1018 copies, and the port shares it with the house's port
// of SA1018 as staticcheck_call.RepeatZeroTimes. Like the original it reads the limit as a constant only when the
// argument is one, not through a phi as other checks of the kind do, so a limit of zero on every path into the call
// is not reported. It departs from the original where the port is right: on a constant with no value the original
// panics reading its kind, and the port judges nothing there; a constant of an integer type always holds a value, so
// no fixture can show it. The registry runs the port in the original's place, under its name, and the original stays
// only as the oracle: the parity tests beside this file run both over staticcheck's own fixture and the house's. The
// two differ nowhere: staticcheck_call departs from callcheck only on calls the FindAll methods are never part of, as
// testdata/src/differences records.
package find_all_zero_limit

import (
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to a FindAll method of *regexp.Regexp.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"(*regexp.Regexp).FindAll":                    staticcheck_call.RepeatZeroTimes("a FindAll method", 1),
	"(*regexp.Regexp).FindAllIndex":               staticcheck_call.RepeatZeroTimes("a FindAll method", 1),
	"(*regexp.Regexp).FindAllString":              staticcheck_call.RepeatZeroTimes("a FindAll method", 1),
	"(*regexp.Regexp).FindAllStringIndex":         staticcheck_call.RepeatZeroTimes("a FindAll method", 1),
	"(*regexp.Regexp).FindAllStringSubmatch":      staticcheck_call.RepeatZeroTimes("a FindAll method", 1),
	"(*regexp.Regexp).FindAllStringSubmatchIndex": staticcheck_call.RepeatZeroTimes("a FindAll method", 1),
	"(*regexp.Regexp).FindAllSubmatch":            staticcheck_call.RepeatZeroTimes("a FindAll method", 1),
	"(*regexp.Regexp).FindAllSubmatchIndex":       staticcheck_call.RepeatZeroTimes("a FindAll method", 1),
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1010",
	"SA1010",
	"Flags a FindAll method of *regexp.Regexp called with n == 0, which always returns no results: staticcheck's "+
		"SA1010, ported onto the engine's own intermediate representation.",
	&Listener,
)
