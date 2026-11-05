// Package allowed holds vars written the way the rule asks, and initializers that say something, none of which the
// rule reports.
package allowed

import "errors"

// A typed var with no initializer is the form the rule asks for.
var count int

var readError error

// An initializer that is not the zero says something.
var (
	retries  int     = 3
	greeting string  = "hello"
	enabled  bool    = true
	scale    float64 = 0.5
	notFound error   = errors.New("not found")
)

// A var with no type names its type through the value, so the value cannot go.
var (
	started  = 0
	empty    = ""
	finished = false
)

// A constant must be initialized, so a zero there restates nothing.
const zero int = 0

// next has a side effect, so its result is no restated zero even when it is zero.
func next() int {
	count++
	return count
}

// Locals: a short declaration, a call, and several values from one call.
func locals() (int, int, int, error) {
	sum := 0
	var drawn int = next()
	var low, high int = bounds()
	return sum, drawn, low + high, readError
}

func bounds() (int, int) {
	return 0, 0
}
