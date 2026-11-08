// Package parity holds every branch of the directive pass in a package that is neither main nor a test, beside the
// clean cases, so the original and the port are compared over each. Its comments say what each side should find.
//
// A //go:debug in the header of a package that is not main, outside a test file: only valid in package main or test.
//go:debug panicnil=1

// Two in one comment group, each reported.
//go:debug panicnil=1
//go:debug http2client=0

// An unknown verb is left alone, and so is //go:build after the header, both clean.
//go:unknownverb anything

/*
//go:debug inside a block comment is not a // comment line, clean.
*/

// A verb ending in a no-break space: invalid space, and the //go:debug judged as well.
//go:debug panicnil=1

// A verb ending in a tab or a plain space is clean, and any other space is reported whatever the verb.
//go:debug	panicnil=1
//go:noinlinex
//go:linknamex
//go:embedx
//go:nosplit x

// The testing hack's `// ERROR` cut: the judgment of the verb is unchanged.
//go:debug panicnil=1 // ERROR "here"
//go:debug // ERROR "here"

// Not a directive at all: a space after the slashes, a capital verb, and no colon.
// go:debug panicnil=1
//Go:debug panicnil=1
//go debug panicnil=1
package parity /* a block adjoining the clause */ //go:debug panicnil=1

// A //go:build verb is not judged, but its space is, here past the header, where it constrains nothing.
//go:build ignore

// After the clause, in a package that is not main: still only valid in package main or test, the first branch that
// holds wins.
//go:debug panicnil=1

func Function() {
	//go:debug panicnil=1
	_ = 0 //go:debug panicnil=1
}
