// Package renamed imports bytes under another name alone, so bytes names nothing here.
package renamed

import b "bytes"

func renamed(first []byte, second []byte) {
	// The original's !bytes.Equal(first, second) names nothing and does not compile, and the fix writes
	// !b.Equal(first, second), the name the call of Compare already uses.
	_ = b.Compare(first, second) != 0 // port moves: should use !bytes.Equal(first, second) instead
	// Parenthesized, the call's function still names b.
	_ = (b.Compare)(first, second) == 0 // port moves: should use bytes.Equal(first, second) instead
}
