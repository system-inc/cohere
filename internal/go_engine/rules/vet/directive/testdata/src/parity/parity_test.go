// A test file of the package: its header is valid.
//go:debug panicnil=1

/* a block comment ends nothing in a Go file */

//go:debug panicnil=1
package parity

// After the clause: only valid before package declaration.
//go:debug panicnil=1

/*
//go:debug in a block comment is clean.
*/
