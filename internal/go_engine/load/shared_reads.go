package load

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	copiedbuild "github.com/system-inc/cohere/internal/go_engine/load/build"
)

// SharedReads is the disk reads every in-process listing of one run shares: each directory's entries and the start of
// each file go/build opens (#yyvstj6), each path's links and kind, which go/build's HasSubdir and IsDir read
// (#5jpss95), and each Go file's header as go/build reads and parses it (#tb538na). A build's listing reads every
// directory its imports reach and every Go file in them, and those reads were most of its CPU: of an extra build's
// listing on cohere's root, 83% was the open, close, directory read and lstat system calls, about 1.1 CPU-s a build,
// every build reading what the host's had just read.
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

	// resolved is each path's filepath.EvalSymlinks, empty where it failed, a string by path, and isDirectories
	// whether each path is a directory, a bool by path, which go/build's HasSubdir and IsDir read (hasSubdirectory,
	// isDirectory): a path's links and kind hold for the run, as its entries do, since the engine adds or removes no
	// file. That is an invariant every fixer has to keep: the wrap and the rename rewrite files and the store writes
	// under a dot directory go never walks, and a fixer that ever adds or removes a file leaves both tables stale.
	// Each is a sync.Map, not under mutex: they are filled once and read for every directory of every build listing
	// at once, so a hit takes no lock.
	resolved      sync.Map
	isDirectories sync.Map

	// headers is each Go file's header as go/build reads and parses it, read once for every listing of the run
	// through the copy of go/build's directory read (#tb538na), and nil when this binary's go/build is not the copy's,
	// when headersUnshared says why every listing parses each header itself.
	headers         *copiedbuild.Headers
	headersUnshared string
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

// NewSharedReads is reads no listing has made yet, each Go file's header among them when this binary links the
// release of go/build the copy is of.
func NewSharedReads() *SharedReads {
	return newSharedReads(runtime.Version())
}

// newSharedReads is NewSharedReads in a binary linked by the Go release linked, which a test names.
func newSharedReads(linked string) *SharedReads {
	shared := &SharedReads{directories: map[string]*sharedDirectory{}, files: map[string]*sharedFile{}}
	if sharing, unshared := copiedbuild.Sharing(linked); sharing {
		shared.headers = copiedbuild.NewHeaders()
	} else {
		shared.headersUnshared = unshared
	}
	return shared
}

// HeadersUnshared is why the run's listings did not share each Go file's header, every build parsing each one
// itself, and empty when they shared them, or when shared is nil, which shares no read at all.
func (shared *SharedReads) HeadersUnshared() string {
	if shared == nil {
		return ""
	}
	return shared.headersUnshared
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
// once for every listing, then from the disk past it. A Go file is opened on disk with no head kept when the run
// shares each header's read (headers): the copy of go/build opens it once a run, and go/build itself only in a
// directory with cgo files, so a head kept for every Go file would be held for the run and read once.
func (shared *SharedReads) openFile(held Held) func(path string) (io.ReadCloser, error) {
	return func(path string) (io.ReadCloser, error) {
		if contents, isHeld := held.lookup(path); isHeld {
			return io.NopCloser(bytes.NewReader(contents)), nil
		}
		if shared.headers != nil && strings.HasSuffix(path, ".go") {
			opened, openError := os.Open(path)
			if openError != nil {
				// A nil interface, as go/build's own openFile returns, never a nil *os.File in one.
				return nil, openError
			}
			return opened, nil
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

// hasSubdirectory is a build.Context's HasSubdir, go/build's own default answered from the run's one table of
// resolved links: the paths as given, then each expanded against the other unexpanded and both expanded, as
// go/build's Context.hasSubdir tries them (build.go in Go 1.27.0, copied). go/build asks it of the toolchain's source
// root and every directory a listing reads, which lies outside it but for the standard library's, so nearly every ask
// resolves both paths' links, a system call per element of each, every build asking what the last one did (#5jpss95).
func (shared *SharedReads) hasSubdirectory(root string, directory string) (string, bool) {
	if relative, found := lexicalSubdirectory(root, directory); found {
		return relative, true
	}
	rootResolved, directoryResolved := shared.resolvedLinks(root), shared.resolvedLinks(directory)
	if relative, found := lexicalSubdirectory(rootResolved, directory); found {
		return relative, true
	}
	if relative, found := lexicalSubdirectory(root, directoryResolved); found {
		return relative, true
	}
	return lexicalSubdirectory(rootResolved, directoryResolved)
}

// resolvedLinks is path with its symbolic links expanded, read once for the run, and empty where expanding failed, as
// go/build's Context.hasSubdir reads filepath.EvalSymlinks with its error ignored.
func (shared *SharedReads) resolvedLinks(path string) string {
	if stored, found := shared.resolved.Load(path); found {
		// Only resolvedLinks stores here, and only strings, so a value of another type is never met.
		if resolved, isString := stored.(string); isString {
			return resolved
		}
	}
	// Ignored: go/build ignores it too, and compares the empty path it leaves, which is below no root.
	resolved, _ := filepath.EvalSymlinks(path)
	// Resolved before it is stored, so no listing waits on another's system calls: two asking at once both resolve
	// it, to the same answer, since nothing in the run moves a link.
	shared.resolved.Store(path, resolved)
	return resolved
}

// isDirectory is a build.Context's IsDir, go/build's own default, whether os.Stat reads path as a directory, links
// followed, so a link to a file is no directory (build.go's Context.isDir in Go 1.27.0, copied), read once for the
// run. go/build asks it of each directory it imports and of every link among a directory's entries, to leave out a
// link to a directory.
func (shared *SharedReads) isDirectory(path string) bool {
	if stored, found := shared.isDirectories.Load(path); found {
		// Only isDirectory stores here, and only bools, so a value of another type is never met.
		if answer, isBool := stored.(bool); isBool {
			return answer
		}
	}
	info, statError := os.Stat(path)
	answer := statError == nil && info.IsDir()
	// As resolvedLinks: stated before it is stored, and two asking at once store the same answer.
	shared.isDirectories.Store(path, answer)
	return answer
}

// lexicalSubdirectory is go/build's hasSubdir (build.go in Go 1.27.0, copied): directory's path below root, slashed,
// and whether it is below root at all, by their cleaned paths alone.
func lexicalSubdirectory(root string, directory string) (string, bool) {
	const separator = string(filepath.Separator)
	prefix := filepath.Clean(root)
	if !strings.HasSuffix(prefix, separator) {
		prefix += separator
	}
	after, found := strings.CutPrefix(filepath.Clean(directory), prefix)
	if !found {
		return "", false
	}
	return filepath.ToSlash(after), true
}
