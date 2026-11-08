//go:build go1.7

package parity

import (
	"encoding/binary"
	"io"
)

func olderBools(writer io.Writer, order binary.ByteOrder) {
	var flag bool
	binary.Write(writer, order, flag)
	binary.Write(writer, order, &flag)
	binary.Write(writer, order, []bool{})
	binary.Write(writer, order, flags{})
	binary.Write(writer, order, int32(0))
	write := func() {
		binary.Write(writer, order, true)
	}
	write()
}
