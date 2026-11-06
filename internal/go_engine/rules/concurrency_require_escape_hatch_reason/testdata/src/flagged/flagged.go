// Package flagged holds escape hatches from Go's checks that give no reason.
package flagged

import (
	"unsafe"
	unchecked "unsafe"
)

type header struct {
	data   uintptr
	length int
}

// handle is a named type whose underlying type is unsafe.Pointer, so converting to it is converting to one.
type handle unsafe.Pointer

func conversions(value *int, raw unsafe.Pointer) {
	_ = unsafe.Pointer(value)    // want "^Converting to `unsafe.Pointer` tells the compiler to trust this code"
	_ = (*header)(raw)           // want "^Converting from `unsafe.Pointer` tells the compiler to trust this code"
	_ = uintptr(raw)             // want "Converting from `unsafe.Pointer`"
	_ = handle(value)            // want "Converting to `unsafe.Pointer`"
	_ = unchecked.Pointer(value) // want "Converting to `unsafe.Pointer`"
}

// A hatch holding another is one finding, reported at the outer one.
func nested(value *int) uintptr {
	return uintptr(unsafe.Pointer(value)) // want "Converting from `unsafe.Pointer`"
}

func calls(text string, bytes []byte, raw unsafe.Pointer) {
	_ = unsafe.String(&bytes[0], len(bytes))     // want "^Calling `unsafe.String` tells the compiler"
	_ = unsafe.StringData(text)                  // want "Calling `unsafe.StringData`"
	_ = unsafe.SliceData(bytes)                  // want "Calling `unsafe.SliceData`"
	_ = unsafe.Add(raw, 8)                       // want "Calling `unsafe.Add`"
	_ = unsafe.Slice(unsafe.StringData(text), 4) // want "Calling `unsafe.Slice`"
}

func cast[Element any](raw unsafe.Pointer) *Element {
	return (*Element)(raw) // want "Converting from `unsafe.Pointer`"
}

var global int

var shared = unsafe.Pointer(&global) // want "Converting to `unsafe.Pointer`"

func markerMidSentence(value *int) {
	// This was once marked Unsafe: mid-sentence, so it is no excuse.
	_ = unsafe.Pointer(value) // want "Converting to `unsafe.Pointer`"
}

func markerInTheWrongCase(value *int) {
	// unsafe: the value outlives the pointer.
	_ = unsafe.Pointer(value) // want "Converting to `unsafe.Pointer`"
}

func markerWithoutColon(value *int) {
	// Unsafe because the value outlives the pointer, but with no colon.
	_ = unsafe.Pointer(value) // want "Converting to `unsafe.Pointer`"
}

func reasonSeparated(value *int) {
	// Unsafe: this is a paragraph away from the conversion below.

	_ = unsafe.Pointer(value) // want "Converting to `unsafe.Pointer`"
}

// A reason trailing the code on the line above speaks for that line, not for the conversion below it.
func reasonTrailingTheLineAbove(value *int) {
	_ = value                 // Unsafe: this speaks for the line it ends.
	_ = unsafe.Pointer(value) // want "Converting to `unsafe.Pointer`"
}

func reasonBelow(value *int) {
	_ = unsafe.Pointer(value) // want "Converting to `unsafe.Pointer`"
	// Unsafe: this is below the conversion, so it speaks for nothing above it.
}

// A reason above one statement does not reach the next.
func reasonForTheStatementBefore(value *int) {
	// Unsafe: the first conversion's value outlives its pointer.
	_ = unsafe.Pointer(value)
	_ = unsafe.Pointer(value) // want "Converting to `unsafe.Pointer`"
}

// A reason for a part of a hatch speaks for that part, not for the hatch that holds it.
func reasonForAnInnerPart(raw unsafe.Pointer) []byte {
	return unsafe.Slice( // want "Calling `unsafe.Slice`"
		// Unsafe: raw points at a byte the caller holds, which says nothing of the length.
		(*byte)(raw),
		4,
	)
}

func emptyReason(value *int) {
	// Unsafe:
	_ = unsafe.Pointer(value) // want "^Converting to `unsafe.Pointer` is marked `Unsafe:` and gives no reason\\."
}

func emptyTrailingReason(value *int) {
	_ = unsafe.Pointer(value) /* Unsafe: */ // want "is marked `Unsafe:` and gives no reason"
}

func emptyBlockReason(text string) {
	/* Unsafe: */
	_ = unsafe.StringData(text) // want "^Calling `unsafe.StringData` is marked `Unsafe:` and gives no reason"
}

func inAClosure(value *int) func() unsafe.Pointer {
	return func() unsafe.Pointer {
		return unsafe.Pointer(value) // want "Converting to `unsafe.Pointer`"
	}
}
