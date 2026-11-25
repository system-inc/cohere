package load

import (
	"bytes"
	"io"
	"os"
)

// Held is the bytes a run holds for a file, by the absolute path the load names it, and false when it holds none,
// so the file is read from disk. The held bytes are the file as it is on disk as of the caller's last write of it,
// or as the caller first read it when it has not written it since: never an overlay, a rewrite not yet written, which
// PackagesWithOverlay is for. So every reader that takes them reads what it would have read from disk, without
// opening the file again. They are shared with the caller and every other reader, so a reader must not modify them.
// A nil Held holds nothing, and every file is read from disk.
type Held func(file string) ([]byte, bool)

// lookup is the bytes held holds for file, and false when it holds none or is nil.
func (held Held) lookup(file string) ([]byte, bool) {
	if held == nil {
		return nil, false
	}
	return held(file)
}

// read is file's held bytes, or its bytes on disk when held holds none.
func (held Held) read(file string) ([]byte, error) {
	if contents, isHeld := held.lookup(file); isHeld {
		return contents, nil
	}
	return os.ReadFile(file)
}

// openFile is a build.Context's OpenFile over held: a held file is served from its bytes, and any other is opened on
// disk. It is nil when held is nil, so go/build opens every file itself.
func (held Held) openFile() func(path string) (io.ReadCloser, error) {
	if held == nil {
		return nil
	}
	return func(path string) (io.ReadCloser, error) {
		if contents, isHeld := held(path); isHeld {
			return io.NopCloser(bytes.NewReader(contents)), nil
		}
		return os.Open(path)
	}
}
