// Package uses holds the uses where the port and the original could plausibly disagree, and records no difference,
// since there is none: both keep the same state over the same walk. A deprecated function's literals, a selector
// chain, a file's start clearing the state, a dot import, and a method of a deprecated type are all judged alike.
package uses

import (
	"example.com/differences/library"
	. "example.com/differences/library"
)

// Found by both: a field at the end of a selector chain.
func chain(value library.Outer) int {
	return value.Inner.OldField
}

// Deprecated: its literals may use deprecated objects too.
func deprecatedWithLiteral() func() int {
	return func() int { return library.OldFunction() }
}

// Found by neither: a dot-imported deprecated function, which the original misses.
func dotImported() int {
	return OldFunction()
}

// Found by both, one each: a deprecated type in a signature, and its deprecated method, used from a function that is
// not deprecated.
func methodOfDeprecated(value library.OldType) int {
	return value.OldMethod()
}
