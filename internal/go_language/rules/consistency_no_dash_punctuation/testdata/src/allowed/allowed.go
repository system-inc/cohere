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

// A dash spelled with an escape is data, such as a test's input, and the escape says it is meant, in every form an
// interpreted string can spell it.
var (
	escapedEmDash     = "an escape \u2014 spells it"
	escapedLongEnDash = "a long escape \U00002013 spells it"
	escapedHexEmDash  = "three hex escapes \xe2\x80\x94 spell it"
	escapedOctalDash  = "three octal escapes \342\200\223 spell it"
)

func use() []any {
	return []any{flags, negative, rawWithHyphen, escapedHyphen, escapedEmDash, escapedLongEnDash, escapedHexEmDash,
		escapedOctalDash}
}
