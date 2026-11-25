// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the value
// and the sign, and a math function held in a variable, whose callee is known only once the variable is lifted, or not
// at all when two functions meet. The port and the original find the same here, so the fixture records no difference.
package differences

import "math"

func valueAndSign() (float64, int) {
	return 0, 0
}

func statements(number int) {
	defer math.Ceil(float64(number))
	go math.Floor(float64(number))
	defer func() {
		math.Trunc(float64(number))
	}()
}

func multipleValues() {
	math.IsInf(valueAndSign())
}

func lifted(condition bool, number int) {
	ceil := math.Ceil
	if condition {
		ceil = math.Ceil
	}
	ceil(float64(number))
	choose := math.Ceil
	if condition {
		choose = math.Floor
	}
	choose(float64(number))
}
