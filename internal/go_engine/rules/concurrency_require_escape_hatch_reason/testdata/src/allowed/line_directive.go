package allowed

import (
	"unsafe"
)

// Lines are the file's own, so a //line directive that renumbers the statement leaves the reason directly above it.
// The directive renumbers every line after it, so this case has the file to itself.
func underALineDirective(value *int) unsafe.Pointer {
	// Unsafe: the value outlives the pointer.
//line renumbered.go:1
	return unsafe.Pointer(value)
}
