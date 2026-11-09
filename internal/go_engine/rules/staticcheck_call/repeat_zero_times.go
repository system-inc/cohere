package staticcheck_call

import (
	"fmt"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"go/constant"
)

// RepeatZeroTimes is the rule that marks the call's argument at index argument, a count of how many times the
// function name describes repeats its work, when it is the constant zero, which makes the call do nothing. It is
// staticcheck v0.8.1's sa1010.RepeatZeroTimes, which SA1010 applies to the FindAll methods of *regexp.Regexp and
// SA1018 copies for strings.Replace and bytes.Replace; the house's two ports share this one copy.
//
// Like the original it reads the count as a constant only when the argument is one, not through a phi as
// ExtractConstant does, so a count of zero on every path into the call is not marked. It departs from the original
// on a constant with no value, a nil one, which the original's read of the value's kind panics on and which this
// rule passes over; a constant of an integer type always holds a value, so the departure is never seen.
func RepeatZeroTimes(name string, argument int) Rule {
	return func(call *Call) {
		count := call.Arguments[argument]
		extracted, isConstant := count.Value.(*intermediate_representation.Constant)
		if !isConstant || extracted.Value == nil || extracted.Value.Kind() != constant.Int {
			return
		}
		if value, isExact := constant.Int64Val(extracted.Value); isExact && value == 0 {
			count.Invalid(fmt.Sprintf("calling %s with n == 0 will return no results, did you mean -1?", name))
		}
	}
}
