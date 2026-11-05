package program

import (
	"hash/crc32"
	"os"
	"unsafe"
)

// packWriter appends entries' bytes to a data file through one buffer, tracking where each lands.
type packWriter struct {
	file   *os.File
	offset int64
	buffer []byte
	err    error
}

// add appends contents to the data file and is the entry that says where they land.
//
// Unsafe: contents is viewed as bytes without a copy, and the checksum and the file's Write only read them, so
// the string's bytes never change.
func (writer *packWriter) add(identity fileIdentity, contents string) contentPackEntry {
	entry := contentPackEntry{
		identity: identity,
		offset:   writer.offset,
		length:   int64(len(contents)),
		checksum: crc32.Update(0, castagnoli, unsafe.Slice(unsafe.StringData(contents), len(contents))),
	}
	writer.offset += int64(len(contents))
	if len(writer.buffer)+len(contents) > cap(writer.buffer) {
		writer.flush()
	}
	if len(contents) > cap(writer.buffer) {
		writer.write(unsafe.Slice(unsafe.StringData(contents), len(contents)))
	} else {
		writer.buffer = append(writer.buffer, contents...)
	}
	return entry
}

func (writer *packWriter) flush() {
	writer.write(writer.buffer)
	writer.buffer = writer.buffer[:0]
}

func (writer *packWriter) write(data []byte) {
	if writer.err == nil && len(data) > 0 {
		_, writer.err = writer.file.Write(data)
	}
}

func (writer *packWriter) finish() error {
	writer.flush()
	return writer.err
}
