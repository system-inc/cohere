// Package api declares two variables of a printf function's shape and a field of one, which only a package importing
// api gives a function literal. printf there finds such a literal a wrapper where it forwards to one, and keeps that
// kind for the variable or field in that package alone, since it can export no fact on another package's object, so no
// build's facts hold it.
package api

// Logf logs args by format, as whoever assigns it says.
var Logf func(format string, args ...any)

// Tracef traces args by format, as whoever assigns it says.
var Tracef func(format string, args ...any)

// Logger logs by its Logf.
type Logger struct {
	// Logf logs args by format, as whoever assigns it says.
	Logf func(format string, args ...any)
}
