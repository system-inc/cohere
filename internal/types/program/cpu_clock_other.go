//go:build !(darwin || linux)

package program

import "time"

// threadCpu has no clock to read here, so --timing says its CPU columns are unavailable rather than
// printing zeros. See cpu_clock_unix.go.
func threadCpu() (time.Duration, bool) {
	return 0, false
}

// processCpu has no clock to read here either.
func processCpu() (time.Duration, bool) {
	return 0, false
}
