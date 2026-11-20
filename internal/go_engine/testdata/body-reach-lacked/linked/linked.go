// Package linked names a symbol by go:linkname, so the host records no function uses of it, and every build reaches
// its bodies whole. It holds a constant, which the reach can judge no value spec of.
package linked

import _ "unsafe"

// Scale is the same in every build.
const Scale = 3

// Hidden is pushed under another name.
//
//go:linkname Hidden example.com/lacked/linked.hidden
func Hidden() int {
	return 1
}
