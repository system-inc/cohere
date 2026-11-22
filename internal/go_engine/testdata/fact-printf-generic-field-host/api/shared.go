// Package api declares GenericLogger, whose Logf field holds a function of its type parameter, in a file every build
// compiles. printf finds a field a wrapper where a function literal assigned to it forwards to one, its fact on the
// generic field, and the one such assignment, through GenericLogger[any], sits in a platform file, so the field is a
// printf wrapper in exactly the builds that compile it, its text and file the same in each.
package api

// Logger is a GenericLogger of any values, whose Logf has a printf function's shape. A reader names it rather than
// GenericLogger[any], whose any would take the reader's body out for consistency-no-any's presence before any fact.
type Logger = GenericLogger[any]

// GenericLogger logs values of T by its Logf.
type GenericLogger[T any] struct {
	// Logf logs args by format.
	Logf func(format string, args ...T)
}
