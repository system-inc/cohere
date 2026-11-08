// Package differences holds the expressions where the port and the original could plausibly disagree, and records no
// difference, since there is none: both print operands with staticcheck's printer and judge generated files by the
// name a file was parsed from. A comment inside an operand, a line directive naming another file, and a type
// parameter of a struct holding a float are all judged alike.
package differences

type holder[T any] struct{ value T }

// Found by both: a comment inside one operand, which the printer leaves out.
func commentInside(values []int) bool {
	return values[ /* the first */ 0] == values[0]
}

// Found by both: a type parameter of structs holding no float.
func genericStructs[T holder[int] | holder[string]](value T) bool {
	return value == value
}

// Found by neither: a type parameter of a struct holding a float.
func genericFloatStruct[T holder[float64] | holder[int]](value T) bool {
	return value == value
}

// Found by both: the line directive below names another file, but the check asks by the name this file was parsed
// from, which is not generated.
//
//line other.go:1
func movedZero() bool {
	return 0 == 0
}
