// Package differences holds the operations where the port and the original could plausibly disagree, and records no
// difference, since there is none: both print with staticcheck's printer and find a constant's declaration the same
// way. An expression over several lines, a comment inside an operand, a constant copied from iota under another
// name, and a constant of an embedded struct's package are judged alike.
package differences

import "strconv"

const copied = iota

const named = copied

// Found by both: an operand over two lines, which the message prints as written.
func multiline(x int, y int) int {
	return (x +
		y) | 0
}

// Found by both: a comment inside the left operand, which the printer leaves out.
func commentInside(values []int) int {
	return values[ /* the first */ 0] ^ 0
}

// Found by neither: a constant whose value is another constant named otherwise than iota.
func copiedIota(x int) int {
	return x | named
}

// Found by both: the copy itself is declared as iota.
func directIota(x int) int {
	return x & copied
}

// Found by neither: a zero computed from another package's constant, which is no literal.
func otherPackage(x int) int {
	return x | strconv.IntSize*0
}
