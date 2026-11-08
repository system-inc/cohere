// Package parity holds bitwise operations with zero, written as literals and as constants defined as iota, and the
// operations and constants the check leaves alone, where the port and staticcheck's SA4016 still agree finding for
// finding. The differential test runs both over each and compares.
package parity

type flag int

type small uint8

const zeroFlag = iota

const (
	firstFlag = iota
	secondFlag
)

const typedFlag flag = iota

const plainZero = 0

const pairFirst, pairSecond = iota, iota

const shifted = iota * 2

const (
	_ = iota
	one
)

func literals(x int, b small, f flag, wide int64) {
	// Found by both, one each: and, or and exclusive or with a literal zero.
	_ = x | 0
	_ = x & 0
	_ = x ^ 0
	_ = b | 0
	_ = f & 0
	_ = wide ^ 0
	// Found by both: zero in parentheses, under a sign, in hexadecimal and in octal.
	_ = x | (0)
	_ = x & -0
	_ = x ^ 0x0
	_ = x | 0o0
	// Found by both: an operation of two constants.
	_ = 1 | 0
	// Found by both: a parenthesized left operand, printed as written.
	_ = (x << 5) | 0
}

func notReported(x int, b bool) {
	// Found by neither: zero on the left, another value, other operators.
	_ = 0 | x
	_ = x | 1
	_ = x << 0
	_ = x &^ 0
	_ = x + 0
	_ = b && false
	// Found by neither: an assignment operator is no binary expression.
	x |= 0
	_ = x
}

func iotaConstants(x int, f flag) {
	// Found by both: constants declared alone as iota, first in a group, and typed.
	_ = x | zeroFlag
	_ = x & firstFlag
	_ = f ^ typedFlag
	// Found by both: a constant declared in another file of the package.
	_ = x | otherFileFlag
	// Found by neither: iota taken implicitly, a plain zero, a pair, an expression of iota, and iota of value one.
	_ = x | secondFlag
	_ = x | plainZero
	_ = x | pairFirst
	_ = x | shifted
	_ = x | one
}

func localIota(x int) {
	const local = iota
	// Found by both: a constant declared as iota inside a function.
	_ = x & local
}

// Found by both: a type parameter of integers.
func genericIntegers[T ~int | ~uint8](x T) {
	_ = x & 0
}

// Found by neither: a type parameter that is not only integers.
func genericMixed[T ~int | ~float64](x T) {
	_ = x + 0
}
