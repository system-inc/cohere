// Package allowed holds escape hatches that say why they are safe, wherever the reason may sit.
package allowed

import (
	"unsafe"
)

func reasonAbove(bytes []byte) string {
	// Unsafe: the bytes are never written after this, so the string they back never changes.
	return unsafe.String(&bytes[0], len(bytes))
}

func reasonTrailing(value *int) uintptr {
	return uintptr(unsafe.Pointer(value)) // Unsafe: the address is only printed, never followed.
}

func blockReasonAbove(text string) *byte {
	/* Unsafe: the caller only reads through the pointer. */
	return unsafe.StringData(text)
}

// starredReason holds its reason on a starred continuation line of a block comment.
func starredReason(text string) *byte {
	/*
	 * The text outlives the pointer.
	 * Unsafe: the caller only reads through it.
	 */
	return unsafe.StringData(text)
}

// toBytes views text as bytes without a copy.
//
// Unsafe: the bytes are only read, and the string is immutable, so they never change under the caller.
func toBytes(text string) []byte {
	if text == "" {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(text), len(text))
}

func reasonAboveTheLoop(values []int) uintptr {
	total := uintptr(0)
	// Unsafe: each address is only summed, never followed.
	for index := range values {
		total += uintptr(unsafe.Pointer(&values[index]))
	}
	return total
}

func firstByte(bytes *byte, count int) byte {
	return *bytes
}

func reasonAboveTheArgument(raw unsafe.Pointer) byte {
	return firstByte(
		// Unsafe: raw points at a byte the caller holds.
		(*byte)(raw),
		4,
	)
}

func reasonAtTheEndOfTheStatement(raw unsafe.Pointer, offset uintptr) unsafe.Pointer {
	moved := unsafe.Add(raw,
		offset) // Unsafe: offset stays inside the allocation raw points into.
	return moved
}

func reasonAboveTheIf(raw unsafe.Pointer) bool {
	// Unsafe: the address is only compared, never followed.
	if uintptr(raw) == 0 {
		return true
	}
	return false
}

func reasonInTheInitOfTheIf(value *int) bool {
	if raw := unsafe.Pointer(value); raw != nil { // Unsafe: the address is only compared, never followed.
		return true
	}
	return false
}

func emptyMarkerBesideAReason(value *int) unsafe.Pointer {
	// Unsafe:
	// Unsafe: the value outlives the pointer.
	return unsafe.Pointer(value)
}

var global int

// Unsafe: global lives as long as the program, so its address never dangles.
var shared = unsafe.Pointer(&global)

var (
	// Unsafe: global lives as long as the program, so its address never dangles.
	first = unsafe.Pointer(&global)
	// Unsafe: the same holds for the second.
	second = uintptr(unsafe.Pointer(&global))
)

// Unsafe: every address in this block is global's, which lives as long as the program.
var (
	third  = unsafe.Pointer(&global)
	fourth = unsafe.Pointer(&global)
)

// A hatch in a closure is excused by the reason above the statement that holds the closure.
func reasonAboveTheClosure(value *int) func() unsafe.Pointer {
	// Unsafe: the value outlives every call of the closure.
	return func() unsafe.Pointer {
		return unsafe.Pointer(value)
	}
}

// Unsafe: runtime.nanotime is the monotonic clock without the allocation time.Now makes.
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// fastrand reads the runtime's random source.
//
// Unsafe: runtime.fastrand has kept this signature since it was added, and the shim test pins it.
//
//go:linkname fastrand runtime.fastrand
func fastrand() uint32

/* Unsafe: the runtime's tick counter has no exported form. */
//go:linkname cputicks runtime.cputicks
func cputicks() int64
