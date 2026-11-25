// Package arrays reads a named type's length in a body each build shares, for the body reach (#bed0mnk): Block is an
// array of 97 bytes on the host's platform and a slice on windows, so `len`, `cap` and `unsafe.Sizeof` of it fold to
// constants on the host and type-check to other values on windows, and Counted's length differs on windows too. Each
// body reads a length one way: of a literal, of a variable another file declares, or through a constant.
package arrays

import "unsafe"

// Literal is the length of a Block's literal.
func Literal() int {
	return len(Block{})
}

// Capacity is the capacity of a Block's literal.
func Capacity() int {
	return cap(Block{})
}

// Size is the size of a Block's literal.
func Size() uintptr {
	return unsafe.Sizeof(Block{})
}

// Variable is the length of a Block another file declares.
func Variable() int {
	return len(Held)
}

// Constant is a constant another file folds from a Counted's length.
func Constant() int {
	return Length
}

// Plain is the length of an array no build changes, which the reach leaves unreached and the differential compares.
func Plain() int {
	return len([3]byte{})
}
