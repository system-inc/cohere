package compose

// Ported from eemeli/yaml 2.9.0, dist/errors.js: YAMLError and its two kinds, as the composer constructs
// them. prettifyError, which adds linePos and a code frame, belongs to parseDocument, not the composer.

import (
	"math"
	"strconv"
	"strings"
)

func newYamlParseError(position [2]int, code string, message string) *YamlError {
	return &YamlError{Name: "YAMLParseError", Code: code, Message: message, Position: position}
}

func newYamlWarning(position [2]int, code string, message string) *YamlError {
	return &YamlError{Name: "YAMLWarning", Code: code, Message: message, Position: position}
}

// containsSameValueZero is `array.includes(value)`: SameValueZero, so NaN finds NaN and objects (a
// MergeKey, Date or Binary here) find only themselves.
func containsSameValueZero(values []any, value any) bool {
	for _, each := range values {
		switch typed := value.(type) {
		case float64:
			if eachNumber, isNumber := each.(float64); isNumber &&
				(eachNumber == typed || math.IsNaN(eachNumber) && math.IsNaN(typed)) {
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
func javaScriptString(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return numberToString(typed)
	case string:
		return typed
	}
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
	if value < 0 {
		sign = "-"
		value = -value
	}
	// d.dddde±x: the digits without the dot, and pointPosition, the decimal point's position after the first.
	formatted := strconv.FormatFloat(value, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(formatted, "e")
	digits := strings.Replace(mantissa, ".", "", 1)
	exponent, _ := strconv.Atoi(exponentText)
	pointPosition := exponent + 1
	digitCount := len(digits)
	switch {
	case digitCount <= pointPosition && pointPosition <= 21:
		return sign + digits + strings.Repeat("0", pointPosition-digitCount)
	case 0 < pointPosition && pointPosition <= 21:
		return sign + digits[:pointPosition] + "." + digits[pointPosition:]
	case -6 < pointPosition && pointPosition <= 0:
		return sign + "0." + strings.Repeat("0", -pointPosition) + digits
	}
	exponentSign := "+"
	if pointPosition-1 < 0 {
		exponentSign = "-"
	}
	exponentDigits := strconv.Itoa(int(math.Abs(float64(pointPosition - 1))))
	if digitCount == 1 {
		return sign + digits + "e" + exponentSign + exponentDigits
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + exponentSign + exponentDigits
}
