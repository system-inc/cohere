package main

import (
	"syscall"
	"unsafe"
)

// memoryStatus is Windows' `MEMORYSTATUSEX`, laid out field for field.
type memoryStatus struct {
	length                   uint32
	memoryLoad               uint32
	totalPhysical            uint64
	availablePhysical        uint64
	totalPageFile            uint64
	availablePageFile        uint64
	totalVirtual             uint64
	availableVirtual         uint64
	availableExtendedVirtual uint64
}

var globalMemoryStatus = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// availableMemory is the physical memory Windows reports available, which already counts its standby
// cache as reclaimable.
func availableMemory() (uint64, error) {
	status := memoryStatus{length: uint32(unsafe.Sizeof(memoryStatus{}))}
	// Unsafe: the conversion sits in Call's own argument list, the form that keeps status alive and in place until
	// the call returns, and Windows writes no more than the length status gives, which is its own size.
	if succeeded, _, callError := globalMemoryStatus.Call(uintptr(unsafe.Pointer(&status))); succeeded == 0 {
		return 0, callError
	}
	return status.availablePhysical, nil
}
