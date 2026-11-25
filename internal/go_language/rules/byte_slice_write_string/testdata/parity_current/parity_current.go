// Package parity_current holds the slices whose conversion depends on the language version, in a module of Go 1.22,
// with a file tagged older.
package parity_current

import "io"

type small byte

type smalls []small

// Found twice: in Go 1.22, slices of a type defined on byte convert directly.
func definedOnByte(writer io.Writer, values []small, named smalls) {
	io.WriteString(writer, string(values))
	io.WriteString(writer, string(named))
}

// Not found: a slice whose type is a type parameter has an interface underneath, no slice.
func generic[Slice ~[]byte](writer io.Writer, values Slice) {
	io.WriteString(writer, string(values))
}
