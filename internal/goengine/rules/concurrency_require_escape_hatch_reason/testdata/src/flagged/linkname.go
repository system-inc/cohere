package flagged

import (
	_ "unsafe"
)

// want +2"^Linking to another package's symbol with `//go:linkname` tells the compiler to trust this code"
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// fastrand reads the runtime's random source. Unsafe: mid-sentence, so it is no excuse.
//
// want +2"Linking to another package's symbol"
//
//go:linkname fastrand runtime.fastrand
func fastrand() uint32

// Unsafe: this is a paragraph away from the directive below.

// want +2"Linking to another package's symbol"
//
//go:linkname cputicks runtime.cputicks
func cputicks() int64

var ticks = 0 // Unsafe: this trails the code on the line above, so it speaks for that line.
// want +2"Linking to another package's symbol"
//
//go:linkname walltime runtime.walltime
func walltime() (int64, int32)

// Unsafe:
// want +2"^Linking to another package's symbol with `//go:linkname` is marked `Unsafe:` and gives no reason"
//
//go:linkname procyield runtime.procyield
func procyield(cycles uint32)

// want +2"Linking to another package's symbol"
//
//go:linkname	osyield runtime.osyield
func osyield()
