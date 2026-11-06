package allowed

import _ "unsafe"

// A declaration with no body is implemented elsewhere, so it has no body to judge.

// assembled is implemented in bodyless.s.
func assembled()

// nanotime is the runtime's own, linked by name.
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64
