// Package differences holds the comparisons where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same constants, the same type sets and the same judgment of
// generated files. Comparisons turned around before they are judged, a type parameter of a named type, a field of a
// generic struct, and limits in parentheses are all judged alike.
package differences

import "math"

type level uint8

type pair[T any] struct{ value T }

// Found by both, one each: zero and the greatest value on the left, each turned around before it is judged.
func twice(u8 uint8) {
	_ = 0 <= u8
	_ = math.MaxUint8 >= u8
}

// Found by both: a type parameter whose one core type is a named unsigned type.
func genericNamed[T level](value T) {
	_ = value > math.MaxUint8
}

// Found by neither: a comparison inside a type the check never reaches as an integer.
func generic(value pair[uint8]) {
	_ = value.value > 254
}

// Found by both on the second line only: parentheses hide the name of a limit from the check, but not a literal.
func parentheses(i8 int8) {
	_ = i8 > (math.MaxInt8)
	_ = i8 > (127)
}
