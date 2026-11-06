package program

import (
	"encoding/binary"
	"io"
)

// indexReader reads little-endian fields, remembering the first read past the end.
type indexReader struct {
	data      []byte
	position  int
	readError error
}

func (reader *indexReader) bytes(length int) []byte {
	if reader.readError != nil || length < 0 || reader.position+length > len(reader.data) {
		reader.readError = io.ErrUnexpectedEOF
		return nil
	}
	read := reader.data[reader.position : reader.position+length]
	reader.position += length
	return read
}

func (reader *indexReader) uint16() uint16 {
	if read := reader.bytes(2); read != nil {
		return binary.LittleEndian.Uint16(read)
	}
	return 0
}

func (reader *indexReader) uint32() uint32 {
	if read := reader.bytes(4); read != nil {
		return binary.LittleEndian.Uint32(read)
	}
	return 0
}

func (reader *indexReader) uint64() uint64 {
	if read := reader.bytes(8); read != nil {
		return binary.LittleEndian.Uint64(read)
	}
	return 0
}
