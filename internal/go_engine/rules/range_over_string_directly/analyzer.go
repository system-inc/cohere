// Package range_over_string_directly is staticcheck's S1029, a range over a string converted to a slice of runes
// that drops the index, which ranging over the string directly does without allocating the slice, ported onto
// staticcheck's intermediate representation as a rule.FunctionCheck.
//
// The original, honnef.co/go/tools@v0.8.1 simple/s1029/s1029.go, runs staticcheck's
// sharedcheck.CheckRangeStringRunes, the very function SA6003 runs as well, and so does the port: it is ported once,
// as staticcheck_code.RangeStringRunes, proven beside both originals there, and this package only names it S1029, as
// range_over_rune_conversion names it SA6003. The original keeps nothing from one function to the next, so judging
// one at a time finds the same; it reports with the pass's Reportf, at the start of the range statement, with no end,
// no fix and no filter on generated files, and so does the port, word for word. The registry still runs the
// original, which stays the oracle: the parity tests beside this file run both over the same packages, and the two
// differ nowhere they hold.
package range_over_string_directly

import (
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. The original declares no
// flags, so neither does the port.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"S1029",
	"Flags ranging over a string converted to a slice of runes when the index goes unused: staticcheck's S1029, "+
		"ported.",
	staticcheck_code.RangeStringRunes,
)
