// Package user reads arrays's lengths across the package boundary, each the way arrays's own bodies do.
package user

import (
	"unsafe"

	"example.com/arrays"
)

// Literal is the length of a Block's literal.
func Literal() int {
	return len(arrays.Block{})
}

// Capacity is the capacity of a Block's literal.
func Capacity() int {
	return cap(arrays.Block{})
}

// Size is the size of a Block's literal.
func Size() uintptr {
	return unsafe.Sizeof(arrays.Block{})
}

// Variable is the length of a Block arrays declares.
func Variable() int {
	return len(arrays.Held)
}

// Constant is a constant arrays folds from a Counted's length.
func Constant() int {
	return arrays.Length
}

// Plain is the length of an array no build changes, which the reach leaves unreached and the differential compares.
func Plain() int {
	return len([3]byte{})
}
