//go:build go1.23

package parity_current

import (
	"encoding/binary"
	"io"
)

func newerBools(writer io.Writer, order binary.ByteOrder) {
	binary.Write(writer, order, true)
	binary.Write(writer, order, struct{ flag bool }{})
}
