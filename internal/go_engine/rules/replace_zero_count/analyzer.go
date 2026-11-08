// Package replace_zero_count is staticcheck's SA1018, strings.Replace or bytes.Replace called with n == 0, which
// replaces nothing, ported onto staticcheck_call, the house's translation of the callcheck framework the original
// rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1018/sa1018.go, whose rules it keeps,
// one for each of the two functions, and whose message it reports word for word over the same range, the count
// argument's expression. The original keeps no state from one function to the next, nor does the port. The
// original's rule is a private copy of SA1010's exported RepeatZeroTimes, and the port shares the one the house's
// port of SA1010 rides, staticcheck_call.RepeatZeroTimes, so this port lands after that one. Like the original it
// reads the count as a constant only when the argument is one, not through a phi as other checks of the kind do, so a
// count of zero on every path into the call is not reported. It departs from the original where the port is right:
// on a constant with no value the original panics reading its kind, and the port judges nothing there; a constant of
// an integer type always holds a value, so no fixture can show it. The original stays the oracle: the parity tests
// beside this file run both over staticcheck's own fixture and the house's. The two differ nowhere: staticcheck_call
// departs from callcheck only on calls the two functions are never part of, as testdata/src/differences records.
package replace_zero_count

import (
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Check judges every call in a function to strings.Replace or bytes.Replace.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"strings.Replace": staticcheck_call.RepeatZeroTimes("strings.Replace", 3),
	"bytes.Replace":   staticcheck_call.RepeatZeroTimes("bytes.Replace", 3),
})

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA1018",
	"Flags strings.Replace or bytes.Replace called with n == 0, which replaces nothing: staticcheck's SA1018, ported "+
		"onto staticcheck's intermediate representation.",
	Check,
)
