package load

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// sharedFile is the start of one file, read once: its first sharedFileHead bytes, whether they are the whole file,
// and the file's size and modification time as its head was read. A reader that wants more reads the rest from the
// disk itself, and only while the file is still the one its head came from (headReader).
type sharedFile struct {
	once     sync.Once
	head     []byte
	complete bool
	size     int64
	modified time.Time
	failure  error
}

// headBuffers are the buffers readHead reads a head into. A file shorter than its head keeps a copy of what it read
// and hands the buffer back, so the next file's read takes it rather than making another; a file at least a head long
// keeps the buffer itself as its head, held by its sharedFile for the run, and never hands it back.
var headBuffers = sync.Pool{New: func() any { return new([sharedFileHead]byte) }}

// readHead reads the file's first sharedFileHead bytes, whether they are all of it, and its size and modification time
// from the file it read them from.
func (file *sharedFile) readHead(path string) error {
	opened, openError := os.Open(path)
	if openError != nil {
		return openError
	}
	defer opened.Close() // Ignored: the file was only read, so a failed close loses nothing.
	info, statError := opened.Stat()
	if statError != nil {
		return statError
	}
	file.size, file.modified = info.Size(), info.ModTime()
	buffer, pooled := headBuffers.Get().(*[sharedFileHead]byte)
	if !pooled {
		// Only readHead puts into headBuffers, and only what its New makes, so this is never met.
		buffer = new([sharedFileHead]byte)
	}
	read, readError := io.ReadFull(opened, buffer[:])
	switch {
	case errors.Is(readError, io.EOF) || errors.Is(readError, io.ErrUnexpectedEOF):
		// A copy of what was read, so a short file holds its own bytes and not a whole head's. The copy is the buffer's
		// last reader, so it goes back for the next file.
		file.head, file.complete = bytes.Clone(buffer[:read]), true
		headBuffers.Put(buffer)
	case readError != nil:
		// Nothing read is kept, so nothing holds the buffer.
		headBuffers.Put(buffer)
		return readError
	default:
		// The head is the buffer itself, which the sharedFile holds for every listing's reader (headReader) for the
		// rest of the run, so it is never handed back.
		file.head = buffer[:]
	}
	return nil
}
