package compose

// Ported from eemeli/yaml 2.9.0, dist/errors.js: YAMLError and its two kinds, as the composer constructs
// them. prettifyError, which adds linePos and a code frame, belongs to parseDocument, not the composer.

import (
	"math"
	"strconv"
	"strings"
)

// YAMLError is a YAMLParseError or a YAMLWarning.
type YAMLError struct {
	// Name is "YAMLParseError" or "YAMLWarning".
	Name string
	// Code is upstream's ErrorCode, such as "BAD_INDENT" or "MISSING_CHAR".
	Code    string
	Message string
	// Pos is the [start, end] of the error in UTF-16 code units.
	Pos [2]int
}

func (yamlError *YAMLError) Error() string { return yamlError.Message }

func newYAMLParseError(position [2]int, code string, message string) *YAMLError {
	return &YAMLError{Name: "YAMLParseError", Code: code, Message: message, Pos: position}
}

func newYAMLWarning(position [2]int, code string, message string) *YAMLError {
	return &YAMLError{Name: "YAMLWarning", Code: code, Message: message, Pos: position}
}

// errorString is an Error upstream throws, whose message a caller catches and reports.
type errorString string

func (message errorString) Error() string { return string(message) }

// containsSameValueZero is `array.includes(value)`: SameValueZero, so NaN finds NaN and objects (a
// MergeKey, Date or Binary here) find only themselves.
func containsSameValueZero(values []ScalarValue, value ScalarValue) bool {
	for _, each := range values {
		switch typed := value.(type) {
		case ScalarNumber:
			e, isNumber := each.(ScalarNumber)
			if isNumber && (e == typed || math.IsNaN(float64(e)) && math.IsNaN(float64(typed))) {
				return true
			}
		case Date, Binary:
			// Objects: a fresh one every time, never equal to another.
		default:
			if each == value {
				return true
			}
		}
	}
	return false
}

// javaScriptString is `${value}` for a primitive scalar value: null, a boolean, a number or a string.
// Only those can be found again by containsSameValueZero, so only those reach it.
func javaScriptString(value ScalarValue) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case ScalarBoolean:
		if typed {
			return "true"
		}
		return "false"
	case ScalarNumber:
		return numberToString(float64(typed))
	case ScalarString:
		return string(typed)
	}
	// Invariant: the one caller, omapTag, converts a key's value only once containsSameValueZero has
	// found it among the earlier keys, and a MergeKey, Date or Binary is a fresh object for every scalar,
	// found by no other, so only the four primitives above get here.
	panic("compose: no string conversion for this value")
}

// numberToString is Number.prototype.toString() in radix 10: the shortest digits that round-trip, in
// plain notation for exponents from -7 to 20 and exponential notation beyond.
func numberToString(value float64) string {
	switch {
	case math.IsNaN(value):
		return "NaN"
	case value == 0:
		return "0"
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	}
	sign := ""
	magnitude := value
	if value < 0 {
		sign = "-"
		magnitude = -value
	}
	// d.dddde±x: the digits without the dot, and n, the decimal point's position after the first.
	formatted := strconv.FormatFloat(magnitude, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(formatted, "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	// Ignored: exponentText is the exponent strconv.FormatFloat just wrote in its 'e' format, a sign
	// and decimal digits, which Atoi always reads.
	exponent, _ := strconv.Atoi(exponentText)
	n := exponent + 1
	k := len(digits)
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	exponentSign := "+"
	if n-1 < 0 {
		exponentSign = "-"
	}
	exponentDigits := strconv.Itoa(int(math.Abs(float64(n - 1))))
	if k == 1 {
		return sign + digits + "e" + exponentSign + exponentDigits
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + exponentSign + exponentDigits
}
