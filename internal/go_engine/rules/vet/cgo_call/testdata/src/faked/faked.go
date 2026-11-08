// Package faked includes a header that does not exist, so the cgo tool fails on it, and the load type-checks its
// files as written, with package "C" faked, and its types importing runtime/cgo as a translation's would. Both
// judgments read the files as written, which are the files compiled.
package faked

// #include "missing.h"
import "C"

import "unsafe"

// Faked hands C a slice and a number, through a function the missing header would declare.
func Faked() {
	var values []int
	C.take(unsafe.Pointer(&values)) // finds
	var number int
	C.take(unsafe.Pointer(&number))
	C.take(*(*unsafe.Pointer)(unsafe.Pointer(&values))) // finds
	var holder Holder
	C.take(unsafe.Pointer(&holder)) // finds
}

// Holder holds a map.
type Holder struct{ Table map[int]int }
