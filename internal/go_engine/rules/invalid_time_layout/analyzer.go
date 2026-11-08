// Package invalid_time_layout is staticcheck's SA1002, a layout given as a constant to time.Parse that is not written
// in Go's reference time, ported onto staticcheck_call, the house's translation of the callcheck framework the
// original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1002/sa1002.go, whose rule it keeps and
// whose messages, the time package's own error, it reports word for word over the same range, the layout argument's
// expression. It finds the layout's position in staticcheck's knowledge package, a table of standard library argument
// positions that the house ports in a later wave. The original keeps no state from one function to the next, nor
// does the port. The original stays the oracle: the parity tests beside this file run both over staticcheck's own
// fixture and the house's. The two differ nowhere: staticcheck_call departs from callcheck only on calls time.Parse
// is never part of, as testdata/src/differences records.
package invalid_time_layout

import (
	"go/constant"
	"strings"
	"time"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Check judges every call in a function to time.Parse.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"time.Parse": checkLayout,
})

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA1002",
	"Flags a layout given as a constant to time.Parse that is not written in Go's reference time: staticcheck's "+
		"SA1002, ported onto staticcheck's intermediate representation.",
	Check,
)

// checkLayout marks the call's layout with the error parsing the layout as its own value gives, when the layout is a
// constant string. A layout is its own value only when it is written in the reference time, so parsing it as one
// fails exactly when it is not.
func checkLayout(call *staticcheck_call.Call) {
	layout := call.Arguments[knowledge.Arg("time.Parse.layout")]
	extracted := staticcheck_call.ExtractConstantOfKind(layout.Value, constant.String)
	if extracted == nil {
		return
	}
	// An underscore pads a day with a space and a Z prints Z for UTC, neither of which parses back as itself, so
	// both are put in the forms that do before the layout is parsed.
	value := strings.ReplaceAll(constant.StringVal(extracted.Value), "_", " ")
	value = strings.ReplaceAll(value, "Z", "-")
	if _, parseError := time.Parse(value, value); parseError != nil {
		layout.Invalid(parseError.Error())
	}
}
