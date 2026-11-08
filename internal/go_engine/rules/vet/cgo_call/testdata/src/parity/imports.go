package parity

// The imports the file type-checked again keeps: a renamed one and a dot import, each resolved to the package the
// file compiled imported.

// static void take(void *pointer) {}
import "C"

import (
	. "fmt"
	pointers "unsafe"
)

// Imported hands C values converted through a renamed unsafe, and calls what a dot import brings.
func Imported() {
	var values []int
	C.take(pointers.Pointer(&values)) // finds
	var number int
	C.take(pointers.Pointer(&number))
	Println(pointers.Pointer(&values))
	C.take(*(*pointers.Pointer)(pointers.Pointer(&values))) // finds
}
