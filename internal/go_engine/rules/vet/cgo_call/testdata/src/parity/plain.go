package parity

import "unsafe"

// This file imports no "C", so it is not judged, and is not type-checked again: it declares the types and variables
// the cgo files use.

// Holder holds a slice, which the rules forbid passing, and a count, which they allow.
type Holder struct {
	Values []int
	Count  int
}

// Embedding embeds Holder, so it holds a slice through an embedded field.
type Embedding struct {
	Holder
}

// Tree is a recursive type that holds a slice of its own pointers.
type Tree struct {
	Children []*Tree
}

// List is a recursive type that holds only a pointer to itself, which the judgment visits once.
type List struct {
	Next  *List
	Value int
}

// Channel is a defined channel type.
type Channel chan int

// SliceAlias is an alias of a slice type.
type SliceAlias = []int

// Box is a generic struct, which holds a pointer when its type argument does.
type Box[Content any] struct {
	Content Content
}

// PointerToPointer is a defined pointer type to unsafe.Pointer.
type PointerToPointer *unsafe.Pointer

// PointerAlias is an alias of unsafe.Pointer.
type PointerAlias = unsafe.Pointer

// Defined is a defined type whose underlying type is unsafe.Pointer.
type Defined unsafe.Pointer

// hiddenHolder is unexported, so the files type-checked again do not reach it.
type hiddenHolder struct {
	values []int
}

// Shared is a package-level slice.
var Shared []int

// hiddenShared is unexported, so the files type-checked again do not reach it.
var hiddenShared []int

// Fixed is a package-level array of numbers.
var Fixed [4]int

// Plain calls nothing of C, and neither judgment reads it.
func Plain() {
	_ = hiddenHolder{values: hiddenShared}
	_ = Plain
}
