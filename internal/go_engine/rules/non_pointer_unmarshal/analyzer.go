// Package non_pointer_unmarshal is staticcheck's SA1014, a value that is not a pointer given to the encoding/json or
// encoding/xml functions that unmarshal into it, ported onto staticcheck_call, the house's translation of the
// callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1014/sa1014.go, whose rules it keeps,
// one for each of the five functions and methods, and whose messages it reports word for word over the same range,
// the target argument's expression. The original keeps no state from one function to the next, nor does the port.
// Both judge a target passed as an interface by what was converted to it, since staticcheck_call sees through the
// conversion as callcheck does, so a map stored in an interface variable is reported where the variable is lifted
// into the conversion itself. The registry runs the port in the original's place, under its name, and the original
// stays only as the oracle: the parity tests beside this file run both over staticcheck's own fixture and the house's.
// The two differ nowhere: staticcheck_call departs from callcheck only on calls these functions are never part of, as
// testdata/src/differences records.
package non_pointer_unmarshal

import (
	"fmt"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to a function or method that unmarshals into one of its arguments.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"encoding/xml.Unmarshal":                checkTarget("xml.Unmarshal", 1),
	"(*encoding/xml.Decoder).Decode":        checkTarget("Decode", 0),
	"(*encoding/xml.Decoder).DecodeElement": checkTarget("DecodeElement", 0),
	"encoding/json.Unmarshal":               checkTarget("json.Unmarshal", 1),
	"(*encoding/json.Decoder).Decode":       checkTarget("Decode", 0),
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
	rule.Namespace+"SA1014",
	"SA1014",
	"Flags a value that is not a pointer given to an encoding/json or encoding/xml function that unmarshals into it: "+
		"staticcheck's SA1014, ported onto the engine's own intermediate representation.",
	&Listener,
)

// checkTarget is the rule that marks the argument at index, the target the function name describes unmarshals into,
// when it is not a pointer.
func checkTarget(name string, index int) staticcheck_call.Rule {
	return func(call *staticcheck_call.Call) {
		target := call.Arguments[index]
		if !isPointer(target.Value) {
			target.Invalid(fmt.Sprintf(
				"%s expects to unmarshal into a pointer, but the provided value is not a pointer", name,
			))
		}
	}
}

// isPointer is whether value may hold a pointer: it is one, or an interface, whose dynamic type the check cannot
// know.
func isPointer(value intermediate_representation.Value) bool {
	switch value.Type().Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return true
	}
	return false
}
