// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation, ask the same purity pass, and
// judge a function in a test file by the name of its file. These are the calls where the callee is least plain,
// generic and variadic calls, calls the form builds from conversions and builtins, and pure calls nested in others,
// where a predicate built from the purity pass otherwise than the original reads it would show here as a finding one
// side makes and the other does not.
package differences

import (
	"strconv"
	"strings"
)

type number interface {
	~int | ~int64
}

func twice[T number](value T) T { return value * 2 }

func join(parts ...string) string { return strings.Join(parts, ",") }

// Found by both: the outer of two nested pure calls, whose result is discarded, where the inner one's is read.
func nested() {
	strings.ToUpper(strings.TrimSpace(" text "))
}

// Found by neither: a variadic function, whose parameter is a slice, which the purity pass never judges pure, a
// function of the standard library its list of pure ones does not hold, and a generic function, whose parameter of a
// type parameter's type is no basic type.
func judgedImpure() {
	join("a", "b")
	strconv.Itoa(4)
	twice(3)
}

// Found by neither: conversions and builtins are no calls to a function the purity pass holds.
func notFunctions(values []int) {
	_ = int64(len(values))
	_ = string(rune(65))
}
