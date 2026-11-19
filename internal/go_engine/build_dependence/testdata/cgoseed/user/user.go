// Package user is a plain package that uses the cgo package's exported API, with no C of its own.
package user

import (
	"unsafe"

	"example.com/cgoseed/native"
)

// Count reads Slots by value, 4 on the host and 8 under the tag: dependent.
func Count() int { return native.Slots }

// Size reads Shape's layout, 4 bytes on the host and 16 under the tag: dependent.
func Size() uintptr { return unsafe.Sizeof(native.Shape{}) }

// Control reads Steady, which no build changes, and nothing of the C side: independent.
func Control() int { return native.Steady }
