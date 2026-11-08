package parity

import (
	. "reflect"
	"unsafe"
)

// A dot import of reflect names its Value and headers without their package, and they are judged the same. reflect
// and unsafe each declare a Pointer, so the two dot imports are in files of their own.
func dotImportedReflect(header *SliceHeader, value Value, text StringHeader) {
	_ = unsafe.Pointer(header.Data)
	_ = unsafe.Pointer(value.Pointer())
	_ = unsafe.Pointer(text.Data) // reported
	_ = *header                   // reported
	_ = &StringHeader{}           // reported
	_ = Pointer                   // reflect's Kind, which unsafe's Pointer would collide with
}
