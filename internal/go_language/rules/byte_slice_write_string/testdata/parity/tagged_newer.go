//go:build go1.18

package parity

import "io"

type small byte

// Found: in a file of Go 1.18, a slice of a type defined on byte converts directly.
func definedOnByte(writer io.Writer, values []small) {
	io.WriteString(writer, string(values))
}
