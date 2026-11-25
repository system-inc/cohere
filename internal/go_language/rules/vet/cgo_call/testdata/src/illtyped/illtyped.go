// Package illtyped holds type errors on purpose: the address of a value the type checker refuses to take has no type,
// while the value has one, which is the one shape the judgment of an argument's address, after its type, finds. The
// cgo tool translates it, since it reads Go only for its references to C.
package illtyped

// static void take(void *pointer) {}
// static void takeTwo(void *first, void *second) {}
import "C"

import "unsafe"

// Untyped hands C addresses the type checker refuses: of a map's element, and of a call's result.
func Untyped() {
	var table map[string][]int
	C.take(unsafe.Pointer(&table["key"])) // finds, at the address
	C.take(&table["key"])                 // finds, at the address
	var numbers map[string]int
	C.take(unsafe.Pointer(&numbers["key"]))
	C.take(unsafe.Pointer(&Values())) // finds, at the address
	C.take(unsafe.Pointer(&Number()))
	C.take(unsafe.Pointer(&undeclared))
	var count int
	C.takeTwo(unsafe.Pointer(&count), unsafe.Pointer(&table["key"])) // finds, at the second's address
}

// Values is a slice, which no address can be taken of as a call's result.
func Values() []int { return nil }

// Number is a number.
func Number() int { return 0 }
