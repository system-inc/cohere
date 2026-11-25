//go:build go1.17

package parity_current

import "io"

// Found: a file tagged Go 1.17 is of that version, and a []byte converts in every version.
func olderFile(writer io.Writer, bytes []byte) {
	io.WriteString(writer, string(bytes))
}
