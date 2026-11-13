package build

import (
	"bytes"
	"sync"
	"sync/atomic"
)

// Headers is the reads of Go files' headers every listing of one run shares: readGoInfo's whole result for each file,
// by path, read by the first build to read the file and taken by every build after it finished. A file on disk the
// run does not hold is keyed by its path alone, as the run's shared reads hold its head (load's SharedReads): nothing
// writes such a file in a run. A file the run holds is read again whenever its held bytes are not the ones read, since
// the long-line wrap rewrites held files between the host's load and the extra builds'. The run never writes held
// bytes in place: a write holds new bytes for the file (load's Held), so bytes equal to the ones read are the bytes
// read.
type Headers struct {
	// parsed counts the headers read, which a test reads: one a file however many builds read it, unless its held
	// bytes moved or two builds began reading it at once.
	parsed atomic.Int64

	// reading, when set, is called by a build about to read a file itself, which a test drives two reads of one file
	// at once with, never by timing.
	reading func(name string)

	mutex sync.Mutex
	files map[string]*sharedHeader
}

// sharedHeader is one file's header: whether it is read from held bytes and which, and the first read of it to finish,
// nil until one has.
type sharedHeader struct {
	held   bool
	source []byte
	read   atomic.Pointer[sharedRead]
}

// sharedRead is one read of a file's header: readGoInfo's result, compacted, and the errors opening and reading the
// file.
type sharedRead struct {
	info      fileInfo
	openError error
	readError error
}

// NewHeaders is headers no listing has read yet.
func NewHeaders() *Headers {
	return &Headers{files: map[string]*sharedHeader{}}
}

// Parsed is how many headers headers read: each file once while its bytes hold, but for a file two builds began
// reading at once, which each read.
func (headers *Headers) Parsed() int64 {
	return headers.parsed.Load()
}

// header is the shared read of the file at path, made anew when the one held was read from other bytes than these:
// read from held bytes when these are not, or from other held bytes.
func (headers *Headers) header(path string, held bool, source []byte) *sharedHeader {
	headers.mutex.Lock()
	defer headers.mutex.Unlock()
	read := headers.files[path]
	// bytes.Equal answers at once for the same bytes, by their address, which is every build's after the first, since
	// the run holds one slice for a file until it writes the file.
	if read == nil || read.held != held || held && !bytes.Equal(read.source, source) {
		read = &sharedHeader{held: held, source: source}
		headers.files[path] = read
	}
	return read
}
