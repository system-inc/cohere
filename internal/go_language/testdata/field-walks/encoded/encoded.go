// Package encoded writes an Encoded in binary.
package encoded

import (
	"bytes"
	"encoding/binary"

	"example.com/fieldwalks/platform"
)

// Write writes encoded.
func Write(encoded *platform.Encoded) error {
	var buffer bytes.Buffer
	return binary.Write(&buffer, binary.LittleEndian, encoded)
}
