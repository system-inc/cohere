// Package differences holds the assignments where the port's test of syntax could most plausibly part from the
// pattern: parentheses around the selector and its operand, a pointer to testing.B behind an alias, a field reached
// through an embedding, a selector whose operand is a call, and assignments of more than one value. The port and the
// original find the same here, so the fixture records no difference.
package differences

import "testing"

type alias = testing.B

type holder struct {
	*testing.B
}

func benchmark() *testing.B { return nil }

func shapes(b *testing.B, aliased *alias, held holder) {
	(b.N) = 1
	(b).N = 1
	aliased.N = 1
	held.B.N = 1
	held.N = 1
	benchmark().N = 1
	b.N, b.N = 1, 2
}
