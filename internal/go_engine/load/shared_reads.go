package load

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"
)

// SharedReads is the disk reads every in-process listing of one run shares: each directory's entries and the start of
// each file go/build opens (#yyvstj6). A build's listing reads every directory its imports reach and every Go file in
// them, and those reads were most of its CPU: of an extra build's listing on cohere's root, 83% was the open, close,
// directory read and lstat system calls, about 1.1 CPU-s a build, every build reading what the host's had just read.
// What a directory holds and what a file says do not change with the build's environment; only which files go/build
// selects does, and go/build still selects them in each build, through its own context, from what it reads here. So
// sharing is exact: it changes where the bytes come from, never which.
//
// A run's loads share one SharedReads only once nothing will write a file it read: a file the run holds is always read
// from its held bytes first (Held), which follow every write, and the engine never adds or removes a file, so a
// directory's entries hold for the run. A nil SharedReads shares nothing, and each listing reads the disk itself.
type SharedReads struct {
	// pastHead counts the readers that read on past a file's shared head, opening it on disk, and directoryReads
	// every directory read asked of them, each listing's first read of a directory once.
	pastHead       atomic.Int64
	directoryReads atomic.Int64

	mutex       sync.Mutex
	directories map[string]*sharedDirectory
	files       map[string]*sharedFile
}

// sharedDirectory is one directory's entries, read once.
type sharedDirectory struct {
	once    sync.Once
	entries []fs.FileInfo
	failure error
}

// sharedFileHead is how much of a file is read once and kept: go/build's own read buffer, and in it a Go file's
// imports, nearly always. A reader that wants more, past a long header or to a file's `//go:embed` lines, reads on
// from the disk, so the head bounds the memory, never what a reader can read. On cohere's root an extra build's
// listing reads 10,577 files: 4 KiB heads hold 31 MB and send 319 readers past them, 8 KiB hold 49 MB for 112, and
// 2 KiB 18 MB for 1,055, at about the same CPU (#yyvstj6).
const sharedFileHead = 4 << 10

// NewSharedReads is reads no listing has made yet.
func NewSharedReads() *SharedReads {
	return &SharedReads{directories: map[string]*sharedDirectory{}, files: map[string]*sharedFile{}}
}

// readDirectory is a build.Context's ReadDir: directory's entries as os.ReadDir reads them, read once for every
// listing. go/build reads only an entry's name, whether it is a directory and its type, so an entry is given as
// that, with no stat of its own (entryInfo).
func (shared *SharedReads) readDirectory(directory string) ([]fs.FileInfo, error) {
	shared.directoryReads.Add(1)
	shared.mutex.Lock()
	read := shared.directories[directory]
	if read == nil {
		read = &sharedDirectory{}
		shared.directories[directory] = read
	}
	shared.mutex.Unlock()
	read.once.Do(func() {
		entries, readError := os.ReadDir(directory)
		if readError != nil {
			// go/build's own reading of a directory that fails keeps none of its entries (Context.readDir's caller),
			// so neither does this.
			read.failure = readError
			return
		}
		read.entries = make([]fs.FileInfo, len(entries))
		for index, entry := range entries {
			read.entries[index] = entryInfo{entry}
		}
	})
	return read.entries, read.failure
}

// openFile is a build.Context's OpenFile: a file held holds from its held bytes, and any other from its start, read
// once for every listing, then from the disk past it.
func (shared *SharedReads) openFile(held Held) func(path string) (io.ReadCloser, error) {
	return func(path string) (io.ReadCloser, error) {
		if contents, isHeld := held.lookup(path); isHeld {
			return io.NopCloser(bytes.NewReader(contents)), nil
		}
		shared.mutex.Lock()
		read := shared.files[path]
		if read == nil {
			read = &sharedFile{}
			shared.files[path] = read
		}
		shared.mutex.Unlock()
		read.once.Do(func() { read.failure = read.readHead(path) })
		if read.failure != nil {
			return nil, read.failure
		}
		return &headReader{shared: shared, path: path, head: bytes.NewReader(read.head), offset: int64(len(read.head)),
			complete: read.complete, size: read.size, modified: read.modified}, nil
	}
}

// Held is how many directories the reads hold and how many reads of them were asked, all zero for nil: a directory
// read more often than once was read by a second listing, which shared the first's read.
func (shared *SharedReads) Held() (int, int64) {
	if shared == nil {
		return 0, 0
	}
	shared.mutex.Lock()
	defer shared.mutex.Unlock()
	return len(shared.directories), shared.directoryReads.Load()
}
