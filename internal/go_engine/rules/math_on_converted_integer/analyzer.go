// Package math_on_converted_integer is staticcheck's SA4015, math.Ceil, Floor, Trunc, IsNaN or IsInf given an
// integer converted to a float, whose answer the integer already gives, ported onto staticcheck_call, the house's
// translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4015/sa4015.go, whose rules it keeps
// and whose message it reports word for word over the same range, the call. It reads a type parameter's type set
// through staticcheck's typeutil, as the original does. The registry runs the port in the original's place, under its
// name, and the original stays only as the oracle: the parity tests beside this file run both over staticcheck's own
// fixtures and the house's. The two differ nowhere: staticcheck_call departs from callcheck only where it would panic,
// on calls the math functions are never part of, as testdata/src/differences records.
package math_on_converted_integer

import (
	"fmt"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to a math function whose answer an integer already gives.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"math.Ceil":  checkConvertedInteger,
	"math.Floor": checkConvertedInteger,
	"math.IsNaN": checkConvertedInteger,
	"math.Trunc": checkConvertedInteger,
	"math.IsInf": checkConvertedInteger,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4015",
	"SA4015",
	"Flags math.Ceil, Floor, Trunc, IsNaN or IsInf given an integer converted to a float: staticcheck's SA4015, "+
		"ported onto the engine's own intermediate representation.",
	&Listener,
)

// checkConvertedInteger marks the call when its first argument is converted from an integer, named by the function
// as typeutil.FuncName spells it.
func checkConvertedInteger(call *staticcheck_call.Call) {
	if isConvertedFromInteger(call.Arguments[0].Value) {
		call.Invalid(fmt.Sprintf("calling %s on a converted integer is pointless",
			intermediate_representation.CallName(call.Instruction.Common())))
	}
}

// isConvertedFromInteger is whether value is a conversion from a type every term of whose type set is an integer:
// an integer type, or a type parameter constrained to integers alone. A constant converted while the program is
// compiled is no conversion here, and a type set with no terms, such as any's, holds no integer.
func isConvertedFromInteger(value intermediate_representation.Value) bool {
	conversion, isConversion := value.(*intermediate_representation.Convert)
	if !isConversion {
		return false
	}
	return typeutil.NewTypeSet(conversion.X.Type()).All(func(term *types.Term) bool {
		basic, isBasic := term.Type().Underlying().(*types.Basic)
		return isBasic && basic.Info()&types.IsInteger != 0
	})
}
