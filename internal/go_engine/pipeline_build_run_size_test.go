package go_engine

import (
	"testing"
	"unsafe"
)

// buildRun stays a multiple of 64 bytes, the cache line, on a 64-bit word (#anfjtr3). The must-check census's unit
// grew it from 384 bytes to 392 with one pointer field and a settled warm run paid for it, though the census was off:
// four Shackler rows on #anfjtr3 (Nov 20) put the unit's whole cost, about 1% of cycles, on that growth, and the same
// field moved to the struct's end with the struct padded to 448 bytes read level with main. Why 392 costs and 384 and
// 448 do not is not known, so this holds the shape that read level and not a cause: a field that pushes the size off a
// multiple of 64 pads it back, or is measured.
func TestBuildRunSizeIsAMultipleOfTheCacheLine(test *testing.T) {
	test.Parallel()

	// The rows were read on a 64-bit word, where every pointer is 8 bytes; a 32-bit word's sizes were never measured.
	if unsafe.Sizeof(uintptr(0)) != 8 {
		test.Skip("buildRun's size was measured on a 64-bit word alone")
	}
	size := unsafe.Sizeof(buildRun{})
	test.Logf("buildRun is %d bytes", size)
	if size%64 != 0 {
		test.Errorf("buildRun is %d bytes, not a multiple of 64: pad it to %d, or measure a settled warm run against "+
			"main's (#anfjtr3)", size, (size+63)/64*64)
	}
}
