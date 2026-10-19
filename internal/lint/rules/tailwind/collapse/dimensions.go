// The dimension reader the calc canonicalizations share: `64rem` read as the number 64 and the unit
// `rem`, plus the two JavaScript primitives their output strings depend on, `String(number)` and
// `Math.round`.
//
// Ported from `src/utils/dimensions.ts` at Tailwind 4.3.3. Upstream wraps the parse in a DefaultMap
// so each spelling is parsed once per process; the parse is pure, so this computes it on every call
// and the answers are the same.
package tailwind

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// dimension is upstream's `[value, unit]` tuple.
//
// The unit is empty for upstream's `null`, a unitless number. That is unambiguous because the regexp
// only captures a unit of one letter or more, or `%`, so a present unit is never the empty string,
// and it keeps upstream's `lhs[1] === rhs[1]` and `lhs[1] ?? rhs[1]` as plain string comparison
// and concatenation.
type dimension struct {
	value float64
	// unit is spelled as written, case included: upstream compares units case-sensitively, so
	// `1REM + 2rem` does not fold.
	unit string
}

// parseDimension is upstream's `dimensions.get(input)`: a number and an optional unit, or nil for
// upstream's `null`.
//
// The grammar is `DIMENSION_REGEX = /^(?<value>[-+]?(?:\d*\.)?\d+)(?<unit>[a-z]+|%)?$/i`, scanned
// byte by byte. Without the `u` flag, JavaScript's case-insensitive match folds only through
// characters whose uppercase stays on the same side of 128, so `[a-z]` with `i` is exactly the
// ASCII letters, and `\d` is exactly the ASCII digits. No exponent: `1e3` reads as the number 1 with
// the unit `e3`, which fails the letters-only unit, so it is not a dimension at all.
func parseDimension(input string) *dimension {
	index := 0
	if index < len(input) && (input[index] == '+' || input[index] == '-') {
		index++
	}

	// `(?:\d*\.)?\d+`: a run of digits, then optionally a dot and a run of digits that must not be
	// empty. With no dot, the first run is the required `\d+` and must not be empty either.
	integerStart := index
	for index < len(input) && isDigit(input[index]) {
		index++
	}
	if index < len(input) && input[index] == '.' {
		index++
		fractionStart := index
		for index < len(input) && isDigit(input[index]) {
			index++
		}
		if index == fractionStart {
			return nil
		}
	} else if index == integerStart {
		return nil
	}
	numberEnd := index

	// `(?<unit>[a-z]+|%)?$`
	unit := input[numberEnd:]
	if unit != "" && unit != "%" {
		for unitIndex := range len(unit) {
			if !isAsciiLetter(unit[unitIndex]) {
				return nil
			}
		}
	}

	// `Number(value)`. The grammar above is a subset of what both JavaScript's Number and Go's
	// ParseFloat accept, including `.5`, `+.5` and `-0`. A run of digits too long for a float64 is
	// `Infinity` to JavaScript, and ParseFloat returns the same infinity alongside its range error,
	// so the range error is not a reason to decline. Upstream's `Number.isNaN` guard can never fire
	// on this grammar.
	value, err := strconv.ParseFloat(input[:numberEnd], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil
	}
	return &dimension{value: value, unit: unit}
}

func isAsciiLetter(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
}

// javaScriptNumberString is ECMAScript's `Number::toString(x)` in radix 10, which is what a number
// interpolated into a template literal prints as.
//
// Separate from formatJavaScriptNumber, which serves only non-negative multiples of 0.25 and declines
// at 1e21. Here the folded values are arbitrary products and quotients, so this follows the
// specification's steps in full: `NaN`, `Infinity`, both zeros as `0`, a leading `-`, plain decimal
// while the decimal exponent n is in (-6, 21], and exponent form outside it, spelled `1e+21` and
// `1e-7`. The digits are Go's shortest round-trip digits, which are the digits the specification
// asks for: the fewest that read back as the same value, the closest such when there is a choice.
func javaScriptNumberString(value float64) string {
	switch {
	case math.IsNaN(value):
		return "NaN"
	case value == 0:
		// Both zeros, since `String(-0)` is `0`.
		return "0"
	case value < 0:
		return "-" + javaScriptNumberString(-value)
	case math.IsInf(value, 1):
		return "Infinity"
	}

	// `d.ddde±x`, from which the digit string and the exponent n of the specification are read.
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(scientific, "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	exponent, _ := strconv.Atoi(exponentText)
	digitCount := len(digits)
	decimalExponent := exponent + 1

	switch {
	case digitCount <= decimalExponent && decimalExponent <= 21:
		return digits + strings.Repeat("0", decimalExponent-digitCount)
	case 0 < decimalExponent && decimalExponent <= 21:
		return digits[:decimalExponent] + "." + digits[decimalExponent:]
	case -6 < decimalExponent && decimalExponent <= 0:
		return "0." + strings.Repeat("0", -decimalExponent) + digits
	}

	sign := "+"
	if exponent < 0 {
		sign = "-"
		exponent = -exponent
	}
	if digitCount == 1 {
		return digits + "e" + sign + strconv.Itoa(exponent)
	}
	return digits[:1] + "." + digits[1:] + "e" + sign + strconv.Itoa(exponent)
}

// javaScriptRound is `Math.round`: the nearest integer, with a half rounding toward positive
// infinity, so `Math.round(-2.5)` is -2 where Go's math.Round gives -3.
//
// `value - floor` is exact for every float64, since below 2^52 it is the fractional part and from
// there up the value is already an integer. A result of zero for a negative value is `-0`, as the
// specification asks, though no caller here can tell the two zeros apart.
func javaScriptRound(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	floor := math.Floor(value)
	rounded := floor
	if value-floor >= 0.5 {
		rounded = floor + 1
	}
	if rounded == 0 && math.Signbit(value) {
		return math.Copysign(0, -1)
	}
	return rounded
}
