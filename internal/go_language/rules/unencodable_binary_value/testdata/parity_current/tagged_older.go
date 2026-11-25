//go:build go1.7

package parity_current

import (
	"encoding/binary"
	"io"
)

func olderBools(writer io.Writer, order binary.ByteOrder) {
	var flag bool
	binary.Write(writer, order, flag)
	binary.Write(writer, order, []bool{})
	binary.Write(writer, order, int(0))
}
