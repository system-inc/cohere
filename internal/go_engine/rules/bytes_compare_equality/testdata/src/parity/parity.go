package parity

import (
	"bytes"
	"strings"
)

type comparer struct{}

func (comparer) Compare(first []byte, second []byte) int { return 0 }

// Compare is this package's own, spelled as bytes' is.
func Compare(first []byte, second []byte) int { return 0 }

var compareFunction = bytes.Compare

const zero = 0

func reported(first []byte, second []byte) {
	_ = bytes.Compare(first, second) == 0
	_ = bytes.Compare(first, second) != 0
	_ = bytes.Compare(first[1:], []byte("text")) != 0

	// The pattern sees through parentheses around the call, its function, the zero and the comparison itself, and
	// the finding spans the comparison as written.
	_ = (bytes.Compare(first, second)) == 0
	_ = (bytes.Compare)(first, second) == 0
	_ = bytes.Compare(first, second) == (0)
	_ = (bytes.Compare(first, second)) != (0)
	_ = (bytes.Compare(first, second) == 0)

	// An integer literal in any base, or under unary plus and minus, whose value is zero.
	_ = bytes.Compare(first, second) == 0x0
	_ = bytes.Compare(first, second) == 00
	_ = bytes.Compare(first, second) == 0o0
	_ = bytes.Compare(first, second) == 0b0
	_ = bytes.Compare(first, second) == 0_0
	_ = bytes.Compare(first, second) != -0
	_ = bytes.Compare(first, second) != +0
	_ = bytes.Compare(first, second) == -(0)
	_ = bytes.Compare(first, second) == - -0
	_ = bytes.Compare(first, second) == -+0

	// Each comparison is its own finding.
	_ = bytes.Compare(first, second) == 0 && bytes.Compare(second, first) != 0
	_ = (bytes.Compare(first, second) == 0) == true
	if bytes.Compare(first, second) != 0 {
		return
	}
}

func left(first []byte, second []byte, holder comparer) {
	// The call on the right, an ordering, and a zero that is no integer literal are not the query's shape.
	_ = 0 == bytes.Compare(first, second)
	_ = 0 != bytes.Compare(first, second)
	_ = bytes.Compare(first, second) < 0
	_ = bytes.Compare(first, second) > 0
	_ = bytes.Compare(first, second) <= 0
	_ = bytes.Compare(first, second) >= 0
	_ = bytes.Compare(first, second) == zero
	_ = bytes.Compare(first, second) == 0.0
	_ = bytes.Compare(first, second) == '\x00'
	_ = bytes.Compare(first, second) == 0+0
	_ = bytes.Compare(first, second) == int(0)
	_ = bytes.Compare(first, second) == ^-1
	_ = bytes.Compare(first, second) == 1
	_ = bytes.Compare(first, second) == -1

	// A function or method named Compare that is not bytes', and bytes.Compare held in a variable.
	_ = strings.Compare("first", "second") == 0
	_ = Compare(first, second) == 0
	_ = holder.Compare(first, second) == 0
	_ = compareFunction(first, second) == 0
	local := bytes.Compare
	_ = local(first, second) == 0
}
