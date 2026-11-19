// Package linked names a symbol by go:linkname, so the host records no function uses of it, and every build reaches
// its bodies whole.
package linked

import _ "unsafe"

// Hidden is pushed under another name.
//
//go:linkname Hidden example.com/reach/linked.hidden
func Hidden() int {
	return 1
}
