package parity

import (
	. "bytes"
)

// A dot import calls the package's functions through no qualifier, which both leave alone.
func dotted(x []byte, y []byte) {
	_ = Index(x, y) != -1
	_ = IndexRune(x, 'r') < 0
}
