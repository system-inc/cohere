// Package sort_slice_on_non_slice is staticcheck's SA1028, sort.Slice, sort.SliceStable or sort.SliceIsSorted given
// a value that is no slice, which panics when the program runs, ported onto staticcheck_call, the house's
// translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1028/sa1028.go, whose rules it keeps
// and whose messages it reports word for word over the same range, the first argument's expression. The registry runs
// the port in the original's place, under its name, and the original stays only as the oracle: the parity tests beside
// this file run both over staticcheck's own fixture and the house's. The two differ nowhere: staticcheck_call departs
// from callcheck only where it would panic, on calls the sort functions are never part of, as testdata/src/differences
// records.
package sort_slice_on_non_slice

import (
	"fmt"
	"go/types"

	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to a sort function that sorts its first argument as a slice.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"sort.Slice":         checkSlice,
	"sort.SliceIsSorted": checkSlice,
	"sort.SliceStable":   checkSlice,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1028",
	"SA1028",
	"Flags sort.Slice, sort.SliceStable or sort.SliceIsSorted given a value that is no slice: staticcheck's "+
		"SA1028, ported onto staticcheck's intermediate representation.",
	&Listener,
)

// checkSlice marks the call's first argument, as the kit hands it, the value before its conversion to the
// parameter's interface, when its underlying type is neither a slice nor an interface. A value of an interface type,
// and of a type parameter, whose underlying type is its constraint's interface, may hold a slice when the program
// runs, so only the literal nil is marked there.
func checkSlice(call *staticcheck_call.Call) {
	// The kit judges only a call whose callee is known, so the callee is never nil here.
	callee := call.Instruction.Common().StaticCallee()
	sorted := call.Arguments[0]
	underlying := sorted.Value.Type().Underlying()
	switch underlying.(type) {
	case *types.Interface:
		// Only the nil written at the call is seen: a nil that reaches it through a phi is not, since the original,
		// which leaves looking at a phi's edges to a later version, does not see it either.
		if literal, isConstant := sorted.Value.(*ir.Const); isConstant && literal.Value == nil {
			sorted.Invalid(fmt.Sprintf("cannot call %s on nil literal", callee))
		}
	case *types.Slice:
		// A slice is what the functions sort.
	default:
		sorted.Invalid(fmt.Sprintf("%s must only be called on slices, was called on %s", callee, underlying))
	}
}
