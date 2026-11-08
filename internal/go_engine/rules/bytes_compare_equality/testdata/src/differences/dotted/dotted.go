// Package dotted imports bytes into its own scope alone, so bytes names nothing here.
package dotted

import . "bytes"

func dotted(first []byte, second []byte) {
	// The original's bytes.Equal(first, second) names nothing and does not compile, and the fix writes
	// Equal(first, second), which the dot import brings into the file's scope.
	_ = Compare(first, second) == 0 // port moves: should use bytes.Equal(first, second) instead
	// Parenthesized and negated, the same.
	_ = (Compare)(first, second) != 0 // port moves: should use !bytes.Equal(first, second) instead
}

func shadowedEqual(first []byte, second []byte) {
	Equal := func([]byte, []byte) bool { return false }
	_ = Equal(first, second)
	// Equal here is the function above and the file names bytes by no other name, so no spelling the fix could write
	// calls bytes.Equal, and the original's bytes.Equal(first, second) names nothing. The finding stays.
	_ = Compare(first, second) == 0 // port withholds: Simplify use of bytes.Compare
}

func laterEqual(first []byte, second []byte) {
	// An Equal declared after the comparison does not stand where it stands, so Equal there is still bytes'. The
	// original's bytes.Equal names nothing all the same.
	_ = Compare(first, second) != 0 // port moves: should use !bytes.Equal(first, second) instead
	Equal := 1
	_ = Equal
}
