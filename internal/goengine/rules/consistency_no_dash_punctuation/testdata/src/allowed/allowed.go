// Package allowed holds text the house writes: commas, periods, and the hyphen-minus that joins a compound word or
// starts a flag. None of it is a dash.
package allowed

// Measure returns a well-known value, and the work is done in one pass.
func Measure() int { return 1 }

// Run it with --verbose, or -v for short.

/* a block comment with a hyphen-minus in a well-formed phrase */

var flags = "--verbose -v"

var negative = -1

var rawWithHyphen = `a raw string with a self-evident hyphen-minus`

var escapedHyphen = "an escape spells a hyphen-minus: \x2d"

func use() []any { return []any{flags, negative, rawWithHyphen, escapedHyphen} }
