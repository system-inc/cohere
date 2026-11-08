// Package duplicate_trim_character is staticcheck's SA1024, a cutset given as a constant to strings.Trim,
// strings.TrimLeft or strings.TrimRight that holds a character twice, the mark of a prefix or suffix written where a
// set of characters is read, ported onto staticcheck_call, the house's translation of the callcheck framework the
// original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1024/sa1024.go, whose rules it keeps
// and whose message it reports word for word over the same range, the cutset argument's expression. It sorts the
// cutset's runes with slices.Sort, where the original sorts them through a sort.Interface of its own, and finds the
// same repeats. The original stays the oracle: the parity tests beside this file run both over staticcheck's own
// fixture and the house's. The two differ nowhere: staticcheck_call departs from callcheck only where it would panic,
// on calls the trimming functions are never part of, as testdata/src/differences records.
package duplicate_trim_character

import (
	"go/constant"
	"slices"

	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to a strings function that trims a cutset, its second argument.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"strings.Trim":      checkCutset,
	"strings.TrimLeft":  checkCutset,
	"strings.TrimRight": checkCutset,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1024",
	"SA1024",
	"Flags a constant cutset given to strings.Trim, TrimLeft or TrimRight that holds a character twice: "+
		"staticcheck's SA1024, ported onto staticcheck's intermediate representation.",
	&Listener,
)

// checkCutset marks the call's cutset, its second argument, when it is a constant string that holds a rune twice. A
// cutset computed while the program runs is never judged.
func checkCutset(call *staticcheck_call.Call) {
	cutset := call.Arguments[1]
	if !isUniqueCutset(cutset.Value) {
		cutset.Invalid("cutset contains duplicate characters")
	}
}

// isUniqueCutset is whether value holds no rune twice, true too for a value that is not a constant string. Each byte
// that is not valid UTF-8 reads as utf8.RuneError, so two of them are a repeat, as they are to the original.
func isUniqueCutset(value ir.Value) bool {
	extracted := staticcheck_call.ExtractConstantOfKind(value, constant.String)
	if extracted == nil {
		return true
	}
	runes := []rune(constant.StringVal(extracted.Value))
	slices.Sort(runes)
	for index := 1; index < len(runes); index++ {
		if runes[index-1] == runes[index] {
			return false
		}
	}
	return true
}
