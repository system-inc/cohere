// Package differences imports a package under the name init, which the type checker refuses, so it records no
// package name for that import. The original asserts that it recorded one, unchecked, and panics, judging nothing of
// the package; the port leaves the refused import out of what the files type-checked again import, as the type
// checker left it out of the package, and judges the rest. A trailing comment, port finds and a message, records
// each finding the port alone makes, since the original makes none.
package differences

// static void take(void *pointer) {}
import "C"

import (
	init "fmt"
	"unsafe"
)

// Passed hands C a slice and a number.
func Passed() {
	var values []int
	C.take(unsafe.Pointer(&values)) // port finds: possibly passing Go type with embedded pointer to C
	var number int
	C.take(unsafe.Pointer(&number))
}
