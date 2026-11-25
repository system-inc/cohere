package gate

import "unsafe"

type inner struct{ value int }

type target struct{ held inner }

type pointer *target

// address converts a named pointer type to unsafe.Pointer, which uses every field of the struct it points to and of
// the struct that holds: the host's U1000 records the body flagged, so no build can splice it, and it leaves the
// standing set in windows's build, which differs from the host's only in files address never reads.
func address(value pointer) unsafe.Pointer { return unsafe.Pointer(value) }

// Address keeps address used.
func Address(value pointer) unsafe.Pointer { return address(value) }

// unreferenced is reported in every build, so U1000's findings are never empty either way.
func unreferenced() {}
