// Package invalid_string_conversion_argument is staticcheck's SA1030, a bit size, a number base or a float format
// given as a constant to a strconv function that the function does not accept, ported onto staticcheck_call, the
// house's translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1030/sa1030.go, whose rules it keeps
// and whose messages it reports word for word over the same range, the argument's expression. It reads the position
// of each judged argument from staticcheck's knowledge package, the table of argument positions the original reads,
// which this wave imports as data and a later wave ports. The registry runs the port in the original's place, under its
// name, and the original stays only as the oracle: the parity tests beside this file run both over staticcheck's own
// fixtures and the house's. The two differ nowhere: staticcheck_call departs from callcheck only where it would panic,
// on calls the strconv functions are never part of, as testdata/src/differences records.
package invalid_string_conversion_argument

import (
	"fmt"
	"go/constant"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to a strconv function that takes a bit size, a number base or a float
// format, each argument against what that function accepts.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"strconv.ParseComplex": func(call *staticcheck_call.Call) {
		checkComplexBitSize(call.Arguments[knowledge.Arg("strconv.ParseComplex.bitSize")])
	},
	"strconv.ParseFloat": func(call *staticcheck_call.Call) {
		checkFloatBitSize(call.Arguments[knowledge.Arg("strconv.ParseFloat.bitSize")])
	},
	"strconv.ParseInt": func(call *staticcheck_call.Call) {
		checkBitSizeWithin(call.Arguments[knowledge.Arg("strconv.ParseInt.bitSize")], 0, 64)
		checkBaseOrZero(call.Arguments[knowledge.Arg("strconv.ParseInt.base")])
	},
	"strconv.ParseUint": func(call *staticcheck_call.Call) {
		checkBitSizeWithin(call.Arguments[knowledge.Arg("strconv.ParseUint.bitSize")], 0, 64)
		checkBaseOrZero(call.Arguments[knowledge.Arg("strconv.ParseUint.base")])
	},

	"strconv.FormatComplex": func(call *staticcheck_call.Call) {
		checkFloatFormat(call.Arguments[knowledge.Arg("strconv.FormatComplex.fmt")])
		checkComplexBitSize(call.Arguments[knowledge.Arg("strconv.FormatComplex.bitSize")])
	},
	"strconv.FormatFloat": func(call *staticcheck_call.Call) {
		checkFloatFormat(call.Arguments[knowledge.Arg("strconv.FormatFloat.fmt")])
		checkFloatBitSize(call.Arguments[knowledge.Arg("strconv.FormatFloat.bitSize")])
	},
	"strconv.FormatInt": func(call *staticcheck_call.Call) {
		checkBase(call.Arguments[knowledge.Arg("strconv.FormatInt.base")])
	},
	"strconv.FormatUint": func(call *staticcheck_call.Call) {
		checkBase(call.Arguments[knowledge.Arg("strconv.FormatUint.base")])
	},

	"strconv.AppendFloat": func(call *staticcheck_call.Call) {
		checkFloatFormat(call.Arguments[knowledge.Arg("strconv.AppendFloat.fmt")])
		checkFloatBitSize(call.Arguments[knowledge.Arg("strconv.AppendFloat.bitSize")])
	},
	"strconv.AppendInt": func(call *staticcheck_call.Call) {
		checkBase(call.Arguments[knowledge.Arg("strconv.AppendInt.base")])
	},
	"strconv.AppendUint": func(call *staticcheck_call.Call) {
		checkBase(call.Arguments[knowledge.Arg("strconv.AppendUint.base")])
	},
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1030",
	"SA1030",
	"Flags a constant bit size, number base or float format given to a strconv function that the function does not "+
		"accept: staticcheck's SA1030, ported onto staticcheck's intermediate representation.",
	&Listener,
)

// constantInteger is argument's value when it is a constant integer, and false otherwise. An argument computed while
// the program runs is never judged. A constant too large for an int64 reads as whatever constant.Int64Val gives, as
// it does in the original, which ignores the same flag: no such constant compiles as an argument of type int.
func constantInteger(argument *staticcheck_call.Argument) (int64, bool) {
	extracted := staticcheck_call.ExtractConstantOfKind(argument.Value, constant.Int)
	if extracted == nil {
		return 0, false
	}
	value, _ := constant.Int64Val(extracted.Value)
	return value, true
}

// checkBitSizeOf marks a bit size that is neither first nor second.
func checkBitSizeOf(argument *staticcheck_call.Argument, first int64, second int64) {
	if size, isConstant := constantInteger(argument); isConstant && size != first && size != second {
		argument.Invalid(fmt.Sprintf("'bitSize' argument is invalid, must be either %d or %d", first, second))
	}
}

// checkComplexBitSize marks a complex number's bit size that is neither 64 nor 128.
func checkComplexBitSize(argument *staticcheck_call.Argument) {
	checkBitSizeOf(argument, 64, 128)
}

// checkFloatBitSize marks a float's bit size that is neither 32 nor 64.
func checkFloatBitSize(argument *staticcheck_call.Argument) {
	checkBitSizeOf(argument, 32, 64)
}

// checkBitSizeWithin marks a bit size below minimum or above maximum.
func checkBitSizeWithin(argument *staticcheck_call.Argument, minimum int64, maximum int64) {
	if size, isConstant := constantInteger(argument); isConstant && (size < minimum || size > maximum) {
		argument.Invalid(fmt.Sprintf("'bitSize' argument is invalid, must be within %d and %d", minimum, maximum))
	}
}

// checkBase marks a number base below 2 or above 36, the bases a number is formatted in.
func checkBase(argument *staticcheck_call.Argument) {
	base, isConstant := constantInteger(argument)
	if !isConstant {
		return
	}
	if base < 2 {
		argument.Invalid("'base' must not be smaller than 2")
	}
	if base > 36 {
		argument.Invalid("'base' must not be larger than 36")
	}
}

// checkBaseOrZero marks a number base below 2 or above 36, the bases a number is parsed in, but for 0, which parses
// in the base the number's prefix names.
func checkBaseOrZero(argument *staticcheck_call.Argument) {
	base, isConstant := constantInteger(argument)
	if !isConstant {
		return
	}
	if base < 2 && base != 0 {
		argument.Invalid("'base' must not be smaller than 2, unless it is 0")
	}
	if base > 36 {
		argument.Invalid("'base' must not be larger than 36")
	}
}

// checkFloatFormat marks a format that is none of the verbs a float or a complex number is formatted with. The
// original judges a complex number's format through a function of its own that calls this one; the verbs are the
// same.
func checkFloatFormat(argument *staticcheck_call.Argument) {
	format, isConstant := constantInteger(argument)
	if !isConstant {
		return
	}
	switch format {
	case 'b', 'e', 'E', 'f', 'g', 'G', 'x', 'X':
		// The verbs strconv formats a float with.
	default:
		argument.Invalid(fmt.Sprintf("'fmt' argument is invalid: unknown format %q", rune(format)))
	}
}
