//go:build go1.21

package parity

import (
	"encoding/binary"
	"io"
)

func newerBools(writer io.Writer, order binary.ByteOrder) {
	var flag bool
	binary.Write(writer, order, flag)
	binary.Write(writer, order, []bool{})
	binary.Write(writer, order, flags{})
	binary.Write(writer, order, int(0))
}
