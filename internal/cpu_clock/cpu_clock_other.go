//go:build !(darwin || linux)

// Package cpu_clock reads the kernel's CPU clocks: a thread's, the process's and its children's. Here there is no
// clock to read, so every caller says its CPU is unavailable rather than printing zeros. See cpu_clock_unix.go.
package cpu_clock

import "time"

// Thread has no clock to read here.
func Thread() (time.Duration, bool) {
	return 0, false
}

// Process has no clock to read here.
func Process() (time.Duration, bool) {
	return 0, false
}

// ProcessUserSystem has no clock to read here.
func ProcessUserSystem() (user time.Duration, system time.Duration, known bool) {
	return 0, 0, false
}

// Children has no clock to read here.
func Children() (time.Duration, bool) {
	return 0, false
}
