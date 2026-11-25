// Package bodiless declares a function without a body, which its assembly file would implement.
package bodiless

// Assembled has no body.
func Assembled() int

// Calls calls Assembled.
func Calls() int { return Assembled() }
