// Package differences holds every shape where the port and x/tools' unsafeptr disagree, each recorded on its line as
// a finding only the original makes, dropped, by its exact message, with the reason it is wrong on the line above.
// Each is valid code by the rules of unsafe.Pointer, which the original reports because it decides rules 5 and 6 by
// the receiver's type name, where the port decides by the method or field the expression resolves to.
package differences

import (
	"reflect"
	"unsafe"
)

type embedsValue struct {
	reflect.Value
}

type embedsValuePointer struct {
	*reflect.Value
}

type embedsHeaderPointer struct {
	*reflect.SliceHeader
}

type embedsStringHeaderPointer struct {
	*reflect.StringHeader
}

type embedsEmbedsHeaderPointer struct {
	embedsHeaderPointer
}

type sliceHeaderPointerType *reflect.SliceHeader

type genericHeader[T any] struct {
	*reflect.SliceHeader
	value T
}

// Rule 5 allows the result of reflect.Value's Pointer or UnsafeAddr converted at once, and each call here is that
// method, converted at once; only the way its receiver is reached differs from `value.Pointer()`.
func values(
	value reflect.Value,
	pointer *reflect.Value,
	embedded embedsValue,
	embeddedPointer embedsValuePointer,
	embeddedThroughPointer *embedsValue,
) {
	// reflect.Value's Pointer, called on the Value pointer points to.
	_ = unsafe.Pointer(pointer.Pointer()) // port drops: possible misuse of unsafe.Pointer
	// reflect.Value's UnsafeAddr, called on the Value pointer points to.
	_ = unsafe.Pointer(pointer.UnsafeAddr()) // port drops: possible misuse of unsafe.Pointer
	// reflect.Value's Pointer, promoted from the Value embedded.
	_ = unsafe.Pointer(embedded.Pointer()) // port drops: possible misuse of unsafe.Pointer
	// reflect.Value's UnsafeAddr, promoted from the *reflect.Value embedded.
	_ = unsafe.Pointer(embeddedPointer.UnsafeAddr()) // port drops: possible misuse of unsafe.Pointer
	// reflect.Value's Pointer, promoted from the Value embedded in the struct pointed to.
	_ = unsafe.Pointer(embeddedThroughPointer.Pointer()) // port drops: possible misuse of unsafe.Pointer
	// reflect.Value's Pointer, called as a method expression with value as its receiver.
	_ = unsafe.Pointer(reflect.Value.Pointer(value)) // port drops: possible misuse of unsafe.Pointer
	// reflect.Value's UnsafeAddr, called as a method expression with value as its receiver.
	_ = unsafe.Pointer(reflect.Value.UnsafeAddr(value)) // port drops: possible misuse of unsafe.Pointer
	// reflect.Value's Pointer, called as a method expression of the pointer type with pointer as its receiver.
	_ = unsafe.Pointer((*reflect.Value).Pointer(pointer)) // port drops: possible misuse of unsafe.Pointer
	// reflect.Value's Pointer, called through parentheses around the selector.
	_ = unsafe.Pointer((value.Pointer)()) // port drops: possible misuse of unsafe.Pointer
}

// Rule 6 allows a header's Data converted to a pointer, and the original approximates a header that overlays a real
// slice or string as one reached through a pointer. Each Data here belongs to a header reached through a pointer, as
// `header.Data` with `header *reflect.SliceHeader` does, written another way.
func headers(
	header *reflect.SliceHeader,
	pointerDefined sliceHeaderPointerType,
	throughPointer embedsHeaderPointer,
	throughStringPointer *embedsStringHeaderPointer,
	deep embedsEmbedsHeaderPointer,
	generic *genericHeader[int],
) {
	// The header pointerDefined points to: a defined pointer type, selected through as a pointer is.
	_ = unsafe.Pointer(pointerDefined.Data) // port drops: possible misuse of unsafe.Pointer
	// The header the embedded *reflect.SliceHeader points to.
	_ = unsafe.Pointer(throughPointer.Data) // port drops: possible misuse of unsafe.Pointer
	// The header the embedded *reflect.StringHeader points to, in a struct reached through a pointer.
	_ = unsafe.Pointer(throughStringPointer.Data) // port drops: possible misuse of unsafe.Pointer
	// The header the *reflect.SliceHeader embedded two levels down points to.
	_ = unsafe.Pointer(deep.Data) // port drops: possible misuse of unsafe.Pointer
	// The header the *reflect.SliceHeader embedded in an instantiated generic struct points to.
	_ = unsafe.Pointer(generic.Data) // port drops: possible misuse of unsafe.Pointer
	// The header header points to, dereferenced explicitly, which is header.Data. The dereference itself is reported
	// by both, as a header read as a value.
	_ = unsafe.Pointer((*header).Data) // port drops: possible misuse of unsafe.Pointer
	// The header pointerDefined points to, dereferenced explicitly. The dereference is reported by both.
	_ = unsafe.Pointer((*pointerDefined).Data) // port drops: possible misuse of unsafe.Pointer
}
