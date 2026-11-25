// Package api declares Logger and printer, whose Logf forwards to fmt.Printf, in a file every build compiles. printf
// finds an interface method a wrapper in a file of go1.26 or later where a method that implements it is one, which an
// assignment of an implementation to the interface anywhere in the package shows. That assignment sits in a platform
// file, so Logger.Logf is a printf wrapper in exactly the builds that compile it, its text and file the same in each.
package api

import "fmt"

// Logger logs by a format and its values.
type Logger interface {
	Logf(format string, args ...any)
}

// printer is a Logger that prints, a printf wrapper in every build.
type printer struct{}

// Logf prints args by format.
func (printer) Logf(format string, args ...any) {
	fmt.Printf(format, args...)
}

// Use hands Logf a string for %d, a printf finding in exactly the builds where Logger.Logf is a wrapper.
func Use(logger Logger) { logger.Logf("%d", "x") }
