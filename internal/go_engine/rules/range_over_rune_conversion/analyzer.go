// Package range_over_rune_conversion is staticcheck's SA6003, a range over a string converted to a slice of runes
// that drops the index, where ranging over the string itself yields the same runes without allocating the slice,
// ported onto staticcheck's intermediate representation as a rule.FunctionCheck.
//
// The original runs staticcheck's sharedcheck.CheckRangeStringRunes, from honnef.co/go/tools@v0.8.1
// internal/sharedcheck/lint.go, the very function S1029 runs as well, and so does the port: it is ported once, as
// staticcheck_code.RangeStringRunes, proven beside both of them there, and this package only names it SA6003. The
// original keeps nothing from one function to the next, so judging one at a time finds the same; it reports with the
// pass's Reportf, at the start of the range statement and with no end, and so does the port, word for word. The
// original stays the oracle: the parity tests beside this file run both over the same packages, and they differ
// nowhere.
package range_over_rune_conversion

import (
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA6003",
	"Flags ranging over a string converted to a slice of runes when the index goes unused: staticcheck's SA6003, "+
		"ported onto its intermediate representation.",
	Check,
)

// Check judges one function, as staticcheck_code.RangeStringRunes does for SA6003 and S1029 alike.
func Check(context *rule.Context, function *ir.Function) {
	staticcheck_code.RangeStringRunes(context, function)
}
