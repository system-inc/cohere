package load

import (
	"bytes"
	"io"
	"os"
)

// headReader reads a file's shared head, then, if the reader wants more and the head is not the whole file, the rest
// from the disk, opened only then.
type headReader struct {
	shared   *SharedReads
	path     string
	head     *bytes.Reader
	offset   int64
	complete bool
	rest     *os.File
}

func (reader *headReader) Read(buffer []byte) (int, error) {
	if reader.head.Len() > 0 {
		return reader.head.Read(buffer)
	}
	if reader.complete {
		return 0, io.EOF
	}
	if reader.rest == nil {
		reader.shared.pastHead.Add(1)
		rest, openError := os.Open(reader.path)
		if openError != nil {
			return 0, openError
		}
		if _, seekError := rest.Seek(reader.offset, io.SeekStart); seekError != nil {
			// Ignored: the seek's failure is the error the read reports.
			_ = rest.Close()
			return 0, seekError
		}
		reader.rest = rest
	}
	return reader.rest.Read(buffer)
}

func (reader *headReader) Close() error {
	if reader.rest == nil {
		return nil
	}
	return reader.rest.Close()
}
