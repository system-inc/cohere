// Package runtimecgo imports runtime/cgo of its own, with no file that imports "C", so its types import runtime/cgo
// and there is no cgo file to judge. Its C is a variable, which no judgment reads.
package runtimecgo

import (
	_ "runtime/cgo"
	"unsafe"
)

// C stands where package "C" would.
var C struct{ take func(unsafe.Pointer) }

// Use hands C's function a slice.
func Use() {
	var values []int
	C.take(unsafe.Pointer(&values))
}
