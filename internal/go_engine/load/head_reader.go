package load

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"
)

// headReader reads a file's shared head, then, if the reader wants more and the head is not the whole file, the rest
// from the disk, opened only then, and only while the file is the size and modification time its head was read at: a
// file saved between the two reads would otherwise splice two versions, and go/build would parse a header that never
// existed, where each build once read its own file whole (#yyvstj6).
type headReader struct {
	shared   *SharedReads
	path     string
	head     *bytes.Reader
	offset   int64
	complete bool
	size     int64
	modified time.Time
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
		info, statError := rest.Stat()
		if statError == nil && (info.Size() != reader.size || !info.ModTime().Equal(reader.modified)) {
			statError = fmt.Errorf("%s changed while the run read it: its first %d bytes were read when it was %d "+
				"bytes, modified %s, and it is now %d bytes, modified %s, so its rest is not the same file's",
				reader.path, reader.offset, reader.size, reader.modified.Format(time.RFC3339Nano), info.Size(),
				info.ModTime().Format(time.RFC3339Nano))
		}
		if statError != nil {
			// Ignored: the stat's failure, or the change it found, is the error the read reports.
			_ = rest.Close()
			return 0, statError
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
