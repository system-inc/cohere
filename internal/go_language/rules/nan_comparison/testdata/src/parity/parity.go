// Package parity holds the shapes SA4012 depends on: which operations it judges, and how far it follows an operand
// back to a call of math.NaN. Each function's comment says what the original makes of it; the parity test holds
// only that the port makes the same.
package parity

import "math"

type measure float64

var global = math.NaN()

func use(values ...any) {}

// Found in each: every comparison operator, NaN on either side.
func operators(value float64) {
	use(value == math.NaN())
	use(math.NaN() == value)
	use(value != math.NaN())
	use(value < math.NaN())
	use(math.NaN() <= value)
	use(value >= math.NaN())
}

// Found in each: arithmetic with NaN is a binary operation too, and the original judges every one.
func arithmetic(value float64) {
	use(value + math.NaN())
	use(math.NaN() / 2)
}

// Found: NaN held in a variable, compared later.
func held(value float64) bool {
	nan := math.NaN()
	copied := nan
	return value == copied
}

// Not found: the two branches call NaN twice, so the phi merges two values and flattens to none.
func twoCalls(value float64, condition bool) bool {
	var nan float64
	if condition {
		nan = math.NaN()
	} else {
		nan = math.NaN()
	}
	return value == nan
}

// Found: both branches hold the one call's result, which a phi, should one stand between, flattens back to.
func sameValueBranches(value float64, condition bool) bool {
	nan := math.NaN()
	held := 0.0
	if condition {
		held = nan
	} else {
		held = nan
	}
	return value == held
}

// Found: the builder keeps three phis here, none trivial, each with two different edges: the outer loop's of the
// call and the inner loop's, the inner loop's of the outer's and the branch's, and the branch's of the other two. All
// of them flatten to the one call, and only flattening sees it.
func phisOfOneCall(value float64, rows int, columns int) bool {
	nan := math.NaN()
	for row := 0; row < rows; row++ {
		saved := nan
		for column := 0; column < columns; column++ {
			if column%2 == 0 {
				nan = saved
			}
		}
	}
	return value == nan
}

// Not found: one branch holds NaN, the other does not.
func oneBranch(value float64, condition bool) bool {
	nan := 1.0
	if condition {
		nan = math.NaN()
	}
	return value == nan
}

// Not found: the loop's phi merges the call with the sum the loop computes, so it flattens to none.
func loopPhi(value float64, count int) bool {
	nan := math.NaN()
	result := false
	for index := 0; index < count; index++ {
		result = result || value == nan
		nan = nan + 0
	}
	return result
}

// Not found: a conversion stands between the call and the comparison.
func converted(value float32) bool {
	return value == float32(math.NaN())
}

// Not found: a defined float type converted from NaN.
func definedType(value measure) bool {
	return value == measure(math.NaN())
}

// Not found: math.IsNaN is the right test.
func isNaNCall(value float64) bool {
	return math.IsNaN(value)
}

// Not found: a package-level variable holding NaN is loaded, not called.
func fromGlobal(value float64) bool {
	return value == global
}

// Found: a function value of math.NaN, called; staticcheck's form calls the function itself.
func functionValue(value float64) bool {
	nan := math.NaN
	return value == nan()
}

// Found: a switch case compared with NaN.
func switchCase(value float64) int {
	switch value {
	case math.NaN():
		return 1
	}
	return 0
}

// Found: in a closure.
func closure() func(float64) bool {
	return func(value float64) bool {
		return value > math.NaN()
	}
}

// Found: in a generic function, after a conversion of the type parameter.
func generic[T ~float64](value T) bool {
	return float64(value) == math.NaN()
}

// Not found: NaN inside a complex number.
func complexNumber(value complex128) bool {
	return value == complex(math.NaN(), 0)
}

// Found: a method's comparison.
func (receiver measure) method() bool {
	return float64(receiver) != math.NaN()
}

// Found: a compound assignment's operation.
func compound(value float64) float64 {
	value *= math.NaN()
	return value
}

// Found: a package-level initializer's closure, among the functions both read.
var initializer = func(value float64) bool {
	return value == math.NaN()
}

// Not found: the comparison of two other values.
func unrelated(first float64, second float64) bool {
	return first == second
}
