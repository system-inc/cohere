//go:build darwin || linux

// Package cpu_clock reads the kernel's CPU clocks: a thread's, the process's and its children's, so the TypeScript
// walk's --timing and the Go engine's read one clock the same way.
package cpu_clock

import (
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Thread is the CPU the calling thread has used, user and system, by the kernel's own count. False when the clock
// cannot be read. A goroutine moves between threads, so a caller reading it twice around some work holds the
// goroutine on its thread between the reads (runtime.LockOSThread), or the difference is two threads' clocks.
//
// Measured on darwin/arm64 (2026-10-04): about 100ns a read, in steps of 41ns, and a 50ms sleep costs the sleeping
// thread 28µs of it. Waiting costs nothing, which is the whole reason --timing reads this rather than the wall clock
// (#8qyzmxw).
func Thread() (time.Duration, bool) {
	var spec unix.Timespec
	if clockGettimeError := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &spec); clockGettimeError != nil {
		return 0, false
	}
	return time.Duration(spec.Nano()), true
}

// Process is the CPU the whole process has used, user and system. False when it cannot be read.
func Process() (time.Duration, bool) {
	var usage syscall.Rusage
	if getrusageError := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); getrusageError != nil {
		return 0, false
	}
	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano()), true
}

// Children is the CPU the process's waited-for children have used, user and system, such as a `go list` it ran.
// False when it cannot be read.
func Children() (time.Duration, bool) {
	var usage syscall.Rusage
	if getrusageError := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &usage); getrusageError != nil {
		return 0, false
	}
	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano()), true
}
