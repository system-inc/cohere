// Package parity_current is a module of Go 1.22, from which the go line is the oldest toolchain that builds the
// module, so a file tagged with an older Go version still reads the module's standard library, and tagged_older.go's
// bools are encodable, while one tagged newer, tagged_newer.go, reads its own.
package parity_current

import (
	"encoding/binary"
	"io"
)

func moduleBools(writer io.Writer, order binary.ByteOrder) {
	binary.Write(writer, order, true)
	binary.Write(writer, order, uint(0))
}
