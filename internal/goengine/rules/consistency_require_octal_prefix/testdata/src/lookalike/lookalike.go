// Package lookalike holds shapes that open with a zero, or hold octal digits, and are not legacy octal integers.
package lookalike

import "strconv"

// Floats that open with a zero are decimal, so the scanner's kind decides, not the leading digit.
const (
	half          = 0.5
	sevenAndAHalf = 07.5
	exponent      = 0e1
	zeroPoint     = 00.5
	trailingDot   = 0755.
	hexFloat      = 0x1p-2
)

// An imaginary literal is not an integer, whatever its digits. An integer one with a leading zero, 0755i, is decimal
// in Go, and gofmt drops its zero, so it is not fixtured here.
const (
	imaginaryFloat  = 07.5i
	imaginaryPrefix = 0o755i
)

// Octal escapes in a rune and a string are escapes, not integer literals.
const (
	nul    = '\000'
	escape = "\033[0m"
)

// Octal digits in a string, parsed at run time, are the string's business. The comment's 0755 is prose.
func parseMode(text string) (int64, error) {
	return strconv.ParseInt("0755", 8, 32)
}

// A struct tag holding digits is a string.
type permission struct {
	Mode int `json:"0644"`
}
