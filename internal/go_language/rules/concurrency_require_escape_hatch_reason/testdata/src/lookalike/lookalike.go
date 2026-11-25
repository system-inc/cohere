// Package lookalike holds code that looks like an escape hatch from Go's checks and is none.
package lookalike

import (
	"sync/atomic"
	"unsafe"
)

type record struct {
	name  string
	count int
}

// Sizeof, Alignof and Offsetof are constants about a type and reach no memory.
func measures(value record) uintptr {
	return unsafe.Sizeof(value) + unsafe.Alignof(value) + unsafe.Offsetof(value.count)
}

// An unsafe.Pointer held, passed and compared is no conversion; the hatch is where the pointer was made.
func held(raw unsafe.Pointer, others []unsafe.Pointer) unsafe.Pointer {
	var copied unsafe.Pointer = raw
	if copied == nil || len(others) == 0 {
		return nil
	}
	return others[0]
}

// A nil pointer holds no address.
func nilPointer() unsafe.Pointer {
	return unsafe.Pointer(nil)
}

// handle's underlying type is unsafe.Pointer, so converting between the two crosses nothing.
type handle unsafe.Pointer

func betweenUnsafePointers(given handle) unsafe.Pointer {
	return unsafe.Pointer(given)
}

// Plain conversions of numbers and pointers are checked.
func plainConversions(value int, pointer *int) (uintptr, *int, int64) {
	return uintptr(value), (*int)(pointer), int64(value)
}

type text struct{}

func (text) String() string { return "text" }

func Slice(values []int) []int { return values }

// Functions and methods named like package unsafe's are their own.
func namedLikeUnsafe(values []int) string {
	_ = Slice(values)
	return text{}.String()
}

// atomic.Pointer holds a pointer without any unsafe conversion.
func atomics(value *int) *int {
	var holder atomic.Pointer[int]
	holder.Store(value)
	return holder.Load()
}

// The //go:linkname directive appears in this sentence, and a sentence is no directive.
//
// go:linkname with a space after the slashes is prose too.
func mentionsTheDirective() {}

/*go:linkname fastrand runtime.fastrand*/
func blockComment() {}
