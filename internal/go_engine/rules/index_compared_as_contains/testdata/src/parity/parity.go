// Package parity holds every shape S1003 branches on, each beside its near miss, so the original and the port are
// compared where each judgment could part them.
package parity

import (
	"bytes"
	"strings"
)

const notFound = -1

const typedNotFound int = -1

func comparisons(a string, b string, x []byte, y []byte, minusOne int) bool {
	// Each pairing the table allows, and each it refuses, for strings.Index.
	_ = strings.Index(a, b) > -1
	_ = strings.Index(a, b) != -1
	_ = strings.Index(a, b) == -1
	_ = strings.Index(a, b) >= 0
	_ = strings.Index(a, b) < 0
	_ = strings.Index(a, b) < -1
	_ = strings.Index(a, b) >= -1
	_ = strings.Index(a, b) <= -1
	_ = strings.Index(a, b) > 0
	_ = strings.Index(a, b) == 0
	_ = strings.Index(a, b) != 0
	_ = strings.Index(a, b) <= 0
	_ = strings.Index(a, b) != 1
	_ = strings.Index(a, b) >= 1
	_ = strings.Index(a, b) != -2

	// The right operand is read as a constant, however it is spelled.
	_ = strings.Index(a, b) != (-1)
	_ = strings.Index(a, b) != -(1)
	_ = strings.Index(a, b) != notFound
	_ = strings.Index(a, b) != typedNotFound
	_ = strings.Index(a, b) != int(-1)
	_ = strings.Index(a, b) != -1.0
	_ = strings.Index(a, b) >= -0
	_ = strings.Index(a, b) >= 00
	_ = strings.Index(a, b) >= 0x0
	_ = strings.Index(a, b) >= 1-1
	_ = strings.Index(a, b) != minusOne

	// The left operand is read without looking through parentheses, and only on the left.
	_ = (strings.Index(a, b)) != -1
	_ = -1 != strings.Index(a, b)
	_ = 0 <= strings.Index(a, b)
	_ = (strings.Index(a, b) != -1)
	_ = (strings.Index(a, b) == -1) || a == ""

	// Each function the check knows, and its neighbors it does not.
	_ = strings.IndexAny(a, b) == -1
	_ = strings.IndexRune(a, 'r') < 0
	_ = strings.LastIndex(a, b) != -1
	_ = strings.IndexByte(a, 'b') != -1
	_ = strings.IndexFunc(a, func(r rune) bool { return r == 'r' }) != -1
	_ = strings.LastIndexAny(a, b) >= 0
	_ = bytes.Index(x, y) == -1
	_ = bytes.IndexAny(x, b) < 0
	_ = bytes.IndexRune(x, 'r') >= 0
	_ = bytes.IndexByte(x, 'b') >= 0

	// Arguments of any shape are carried into the replacement.
	_ = strings.Index(strings.ToLower(a), b+"suffix") >= 0
	_ = bytes.Index(append(x, y...), []byte{'a', 'b'}) != -1

	// A call through a value is no selector of a package's name.
	index := strings.Index
	_ = index(a, b) != -1

	// Comparisons inside larger expressions and statements, the outer one left alone when its left is no call.
	_ = strings.Index(a, b) != -1 == true
	_ = a != "" && strings.Index(a, b) == -1
	_ = !(strings.Index(a, b) < 0)
	if strings.IndexRune(a, 'r') > -1 {
		return true
	}
	return strings.Index(a, b) >= 0
}
