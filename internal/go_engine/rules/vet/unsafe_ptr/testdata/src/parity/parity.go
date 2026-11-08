// Package parity holds every branch of unsafeptr's judgment, each with a clean case beside it, and the shapes most
// likely to set the port and x/tools' unsafeptr apart: defined types and aliases of unsafe.Pointer, uintptr and the
// headers, parentheses at every place the judgment looks through them or does not, method values and expressions,
// embedded fields, generics, closures, package-level initializers, and selectors through a package name. Dot imports
// are in dot_imports.go and dot_imports_reflect.go. The differential test runs both over each and compares. A line
// marked `// reported` is one both report; a line marked `// reported twice` holds two findings.
package parity

import (
	"reflect"
	"unsafe"

	"parity/holder"
)

type pointerType unsafe.Pointer

type pointerAlias = unsafe.Pointer

type uintptrType uintptr

type uintptrAlias = uintptr

type sliceHeaderAlias = reflect.SliceHeader

type stringHeaderAlias = reflect.StringHeader

type sliceHeaderPointerAlias = *reflect.SliceHeader

// sliceHeaderPointerType is a defined pointer type, through which `Data` is a field selector, but whose type is
// named rather than a pointer.
type sliceHeaderPointerType *reflect.SliceHeader

type valueAlias = reflect.Value

type embedsValue struct {
	reflect.Value
}

type embedsHeaderPointer struct {
	*reflect.SliceHeader
}

type embedsHeader struct {
	reflect.SliceHeader
}

type generic[T any] struct {
	Data  uintptr
	value T
}

var packageUintptr uintptr

var packagePointer unsafe.Pointer

// A package-level initializer is judged as a function body is.
var packageLevel = unsafe.Pointer(packageUintptr) // reported

var packageLevelSafe = unsafe.Pointer(uintptr(packagePointer) + 1)

var packageLevelHeader = &reflect.SliceHeader{} // reported

func toPointer(address uintptr) unsafe.Pointer {
	return unsafe.Pointer(address) // reported
}

// The conversion's target: unsafe.Pointer, a defined type and an alias of it, parenthesized, and a function that
// takes a uintptr and returns a pointer, which is a call, not a conversion.
func targets(address uintptr) {
	_ = unsafe.Pointer(address)     // reported
	_ = pointerType(address)        // reported
	_ = pointerAlias(address)       // reported
	_ = (unsafe.Pointer)(address)   // reported
	_ = (unsafe.Pointer)((address)) // reported
	_ = toPointer(address)
	_ = unsafe.Pointer(&address)
	_ = pointerType(&address)
	_ = uintptr(address)
	_ = uintptrType(address)
}

// The converted value: a uintptr, a defined type and an alias of it, a constant, and values that are not uintptrs.
func operands(address uintptr, defined uintptrType, aliased uintptrAlias, pointer unsafe.Pointer, number *int) {
	_ = unsafe.Pointer(defined)          // reported
	_ = unsafe.Pointer(aliased)          // reported
	_ = unsafe.Pointer(uintptr(0))       // reported
	_ = unsafe.Pointer(uintptrType(0))   // reported
	_ = unsafe.Pointer(packageUintptr)   // reported
	_ = unsafe.Pointer(holder.Data)      // reported
	_ = unsafe.Pointer(uintptr(address)) // reported
	_ = unsafe.Pointer(pointer)
	_ = unsafe.Pointer(number)
	_ = unsafe.Pointer(nil)
}

// Rule 3: a pointer converted to uintptr and back, with arithmetic. The pointer must come first, and only once.
func arithmetic(pointer unsafe.Pointer, defined pointerType, offset uintptr) {
	_ = unsafe.Pointer(uintptr(pointer))
	_ = unsafe.Pointer(uintptr(defined))
	_ = unsafe.Pointer(uintptrType(pointer))
	_ = unsafe.Pointer(uintptrAlias(pointer))
	_ = unsafe.Pointer((uintptr)(pointer))
	_ = unsafe.Pointer(uintptr((pointer)))
	_ = unsafe.Pointer(uintptr(unsafe.Pointer(&offset)))
	_ = unsafe.Pointer(uintptrType(pointer) + 1)
	_ = unsafe.Pointer(uintptr(pointer) + offset)
	_ = unsafe.Pointer(uintptr(pointer) + 1 + 2)
	_ = unsafe.Pointer(uintptr(pointer) + (1 + 2))
	_ = unsafe.Pointer((uintptr(pointer) + 1) - 2)
	_ = unsafe.Pointer(uintptr(pointer) - offset)
	_ = unsafe.Pointer(uintptr(pointer) &^ 7)
	_ = unsafe.Pointer(uintptr(pointer) + unsafe.Sizeof(offset))
	_ = unsafe.Pointer(uintptr(pointer) * 2)                      // reported
	_ = unsafe.Pointer(uintptr(pointer) / 2)                      // reported
	_ = unsafe.Pointer(uintptr(pointer) | 1)                      // reported
	_ = unsafe.Pointer(uintptr(pointer) & 1)                      // reported
	_ = unsafe.Pointer(uintptr(pointer) ^ 1)                      // reported
	_ = unsafe.Pointer(uintptr(pointer) << 1)                     // reported
	_ = unsafe.Pointer(uintptr(pointer) % 8)                      // reported
	_ = unsafe.Pointer(offset + uintptr(pointer))                 // reported
	_ = unsafe.Pointer(uintptr(pointer) - uintptr(pointer))       // reported
	_ = unsafe.Pointer(uintptr(pointer) &^ uintptr(pointer))      // reported
	_ = unsafe.Pointer((uintptr(pointer) + 1) + uintptr(pointer)) // reported
	_ = unsafe.Pointer(1 + uintptr(pointer) + 2)                  // reported
	_ = unsafe.Pointer(offset + 1)                                // reported
	_ = unsafe.Pointer(-uintptr(pointer))                         // reported
	_ = unsafe.Pointer(^uintptr(pointer))                         // reported
	_ = unsafe.Pointer(uintptr(toPointer(offset)) + 1)
	_ = unsafe.Pointer(uintptr(unsafe.Pointer(uintptr(pointer))) + 1)
}

// Rule 5: the result of reflect.Value's Pointer or UnsafeAddr. The method is matched by name on a receiver of type
// reflect.Value, written as the call's function, and its result must be converted as it is.
func reflectValues(value reflect.Value, pointer *reflect.Value, embedded embedsValue, aliased valueAlias) {
	_ = unsafe.Pointer(value.Pointer())
	_ = unsafe.Pointer(value.UnsafeAddr())
	_ = unsafe.Pointer((value).Pointer())
	_ = unsafe.Pointer((value.Pointer()))
	_ = unsafe.Pointer(aliased.Pointer())
	_ = unsafe.Pointer(reflect.ValueOf(&value).Pointer())
	_ = unsafe.Pointer(reflect.ValueOf(&value).Elem().UnsafeAddr())
	_ = unsafe.Pointer(value.Pointer() - 1)             // reported
	_ = unsafe.Pointer(value.UnsafeAddr() &^ 1)         // reported
	_ = unsafe.Pointer((value.Pointer)())               // reported
	_ = unsafe.Pointer(reflect.Value.Pointer(value))    // reported
	_ = unsafe.Pointer(reflect.Value.UnsafeAddr(value)) // reported
	_ = unsafe.Pointer(pointer.Pointer())               // reported
	_ = unsafe.Pointer((*pointer).Pointer())
	_ = unsafe.Pointer(embedded.Pointer()) // reported
	_ = unsafe.Pointer(embedded.Value.Pointer())
	_ = unsafe.Pointer(uintptr(value.UnsafePointer()))
	_ = unsafe.Pointer(uintptr(value.UnsafePointer()) + 1)
	pointerMethod := value.Pointer
	_ = unsafe.Pointer(pointerMethod())       // reported
	_ = unsafe.Pointer(uintptr(value.Uint())) // reported
}

// Rule 6: the Data field of a header, allowed only through a pointer to one.
func headers(
	slice *reflect.SliceHeader,
	text *reflect.StringHeader,
	sliceValue reflect.SliceHeader,
	aliasedPointer *sliceHeaderAlias,
	aliasedStringPointer *stringHeaderAlias,
	pointerAliased sliceHeaderPointerAlias,
	pointerDefined sliceHeaderPointerType,
	throughPointer embedsHeaderPointer,
	throughValue embedsHeader,
	throughValuePointer *embedsHeader,
	box *generic[int],
	values []byte,
	pointers []*reflect.SliceHeader,
) {
	_ = unsafe.Pointer(slice.Data)
	_ = unsafe.Pointer(text.Data)
	_ = unsafe.Pointer((slice.Data))
	_ = unsafe.Pointer((slice).Data)
	_ = unsafe.Pointer(aliasedPointer.Data)
	_ = unsafe.Pointer(aliasedStringPointer.Data)
	_ = unsafe.Pointer(pointerAliased.Data)
	_ = unsafe.Pointer(pointers[0].Data)
	_ = unsafe.Pointer((*reflect.SliceHeader)(unsafe.Pointer(&values)).Data)
	_ = unsafe.Pointer(throughPointer.SliceHeader.Data)
	_ = unsafe.Pointer(sliceValue.Data)          // reported
	_ = unsafe.Pointer(pointerDefined.Data)      // reported
	_ = unsafe.Pointer(throughPointer.Data)      // reported
	_ = unsafe.Pointer(throughValue.Data)        // reported
	_ = unsafe.Pointer(throughValuePointer.Data) // reported
	_ = unsafe.Pointer(box.Data)                 // reported
	_ = unsafe.Pointer(slice.Data + 8)           // reported
	_ = unsafe.Pointer(uintptr(slice.Len))       // reported
	_ = unsafe.Pointer((*slice).Data)            // reported twice
	_ = unsafe.Pointer(uintptr(unsafe.Pointer(slice.Data)))
}

// A header made or read as a value: dereferenced, its address taken, or a composite literal's address taken.
func headerValues(slice *reflect.SliceHeader, text *reflect.StringHeader, aliased *sliceHeaderAlias, values []byte) {
	_ = *slice                                           // reported
	_ = *text                                            // reported
	_ = *aliased                                         // reported
	_ = (*slice)                                         // reported
	_ = *(*reflect.SliceHeader)(unsafe.Pointer(&values)) // reported
	var header reflect.SliceHeader
	_ = &header                     // reported
	_ = &reflect.StringHeader{}     // reported
	_ = &sliceHeaderAlias{}         // reported
	_ = &(header)                   // reported
	_ = &embedsHeader{}             // not reported, the header is a field
	_ = &header.Data                // not reported, the field is a uintptr
	_ = (*reflect.SliceHeader)(nil) // not reported, a type expression
	_ = new(reflect.SliceHeader)
	var pointers []*reflect.SliceHeader
	_ = pointers
	_ = header
	number := 1
	_ = -number
	_ = &number
	_ = *(&number)
	channel := make(chan reflect.SliceHeader, 1)
	_ = <-channel
	_ = !(number == 1)
}

// Closures, method bodies and generic functions are judged as any body is; a type parameter has no basic type.
func (box *generic[T]) method(address uintptr) unsafe.Pointer {
	inner := func() unsafe.Pointer {
		return unsafe.Pointer(address) // reported
	}
	_ = inner
	return unsafe.Pointer(box.Data) // reported
}

func generics[Address ~uintptr, Pointer ~unsafe.Pointer, Header reflect.SliceHeader | reflect.StringHeader](
	address Address,
	pointer Pointer,
	header *Header,
) {
	_ = unsafe.Pointer(address)
	_ = unsafe.Pointer(uintptr(address)) // reported
	_ = uintptr(pointer)
	_ = unsafe.Pointer(uintptr(unsafe.Pointer(pointer)) + 1)
	_ = *header
	_ = &header
	_ = generic[reflect.SliceHeader]{}.value
	instance := generic[reflect.SliceHeader]{}
	_ = &instance.value // reported
	_ = func() {
		_ = unsafe.Pointer(uintptr(unsafe.Pointer(&address)) * 2) // reported
	}
}
