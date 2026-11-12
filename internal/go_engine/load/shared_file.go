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
	head := make([]byte, sharedFileHead)
	read, readError := io.ReadFull(opened, head)
	switch {
	case errors.Is(readError, io.EOF) || errors.Is(readError, io.ErrUnexpectedEOF):
		// A copy of what was read, so a short file holds its own bytes and not a whole head's.
		file.head, file.complete = bytes.Clone(head[:read]), true
	case readError != nil:
		return readError
	default:
		file.head = head
	}
	return nil
}
