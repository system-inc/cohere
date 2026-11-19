package p

import "unsafe"

// Shared pads itself to 64 bytes past an inner, whose size is 8 on the host and 16 on windows, so Pad is a [56]byte on
// the host and a [48]byte on windows: Shared is dependent, by its underlying.
type Shared struct {
	Pad [64 - unsafe.Sizeof(inner{})]byte
}

// Sized gives an array as long as an inner, a [8]byte on the host and a [16]byte on windows: dependent. Its body names
// no type, so only its signature can make it so.
func Sized() (sized [unsafe.Sizeof(inner{})]byte) {
	return
}

// steady is one int64 in every build.
type steady struct {
	value int64
}

// Steady gives an array as long as a steady, a [8]byte in every build: independent.
func Steady() (sized [unsafe.Sizeof(steady{})]byte) {
	return
}

// Even pads itself to 64 bytes past a steady, a [56]byte in every build: independent.
type Even struct {
	Pad [64 - unsafe.Sizeof(steady{})]byte
}

// Buffered declares a local array of platformSize bytes and a local type of platformSize ints, 4 of each on the host
// and 8 on windows, so its body moves while its signature, which names no type, does not: its unit is dependent, by
// the body alone, and its own bits, which a reader of it reads, stay unset.
func Buffered() int {
	var buffer [platformSize]byte
	type row [platformSize]int
	var cells row
	return len(buffer) + len(cells)
}
