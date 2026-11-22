// Package api declares Logger, whose Logf field holds a function, in a file every build compiles. printf finds a
// field a wrapper where a function literal assigned to it forwards to one, its fact on the field, and the one such
// assignment sits in a platform file, so Logger.Logf is a printf wrapper in exactly the builds that compile it, its
// text and file the same in each.
package api

// Logger logs by its Logf.
type Logger struct {
	// Logf logs args by format.
	Logf func(format string, args ...any)
}
