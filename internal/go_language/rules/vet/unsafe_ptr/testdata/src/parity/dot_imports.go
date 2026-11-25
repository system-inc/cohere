package parity

import (
	. "unsafe"
)

// A dot import of unsafe names unsafe.Pointer and Sizeof without their package, and they are judged the same.
func dotImportedUnsafe(address uintptr, pointer Pointer) {
	_ = Pointer(address) // reported
	_ = Pointer(uintptr(pointer) + 1)
	_ = Pointer(uintptr(pointer) + Sizeof(address))
	_ = Pointer(uintptr(pointer) * Sizeof(address)) // reported
}
