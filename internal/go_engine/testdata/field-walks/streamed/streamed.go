// Package streamed writes a channel of Streameds in binary, which reaches Streamed only through the channel's element.
package streamed

import (
	"bytes"
	"encoding/binary"

	"example.com/fieldwalks/platform"
)

// Write writes values.
func Write(values chan platform.Streamed) error {
	var buffer bytes.Buffer
	return binary.Write(&buffer, binary.LittleEndian, values)
}
