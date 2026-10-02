package rule

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// FileRead is one path a reader asked the file system about, and whether it was there.
//
// Absence is recorded as carefully as presence: a design system that looked for an entry point and did
// not find it depends on that file staying absent, since creating it changes the design system.
type FileRead struct {
	Path    string
	Present bool
}

// RecordingFS is a file system that notes every path it is asked about and refuses to write.
//
// It is how a reader's inputs are observed rather than listed, the discipline the run cache's input
// recorder holds the compiler to, applied to one reader: whatever the design system asked about is in
// its read set, so a cache keyed on that set cannot miss a file it forgot (#pyhm2t2). Writes panic,
// because a reader that writes is not a reader, and a cached verdict cannot replay a side effect.
type RecordingFS struct {
	underlying vfs.FS
	mutex      sync.Mutex
	reads      map[string]bool
}

var _ vfs.FS = (*RecordingFS)(nil)

// NewRecordingFS wraps underlying.
func NewRecordingFS(underlying vfs.FS) *RecordingFS {
	return &RecordingFS{underlying: underlying, reads: map[string]bool{}}
}

// Reads is every path asked about, sorted. A path seen both present and absent keeps present.
func (recording *RecordingFS) Reads() []FileRead {
	recording.mutex.Lock()
	defer recording.mutex.Unlock()
	reads := make([]FileRead, 0, len(recording.reads))
	for path, present := range recording.reads {
		reads = append(reads, FileRead{Path: path, Present: present})
	}
	sort.Slice(reads, func(first, second int) bool { return reads[first].Path < reads[second].Path })
	return reads
}

func (recording *RecordingFS) note(path string, present bool) {
	recording.mutex.Lock()
	recording.reads[path] = recording.reads[path] || present
	recording.mutex.Unlock()
}

func (recording *RecordingFS) refuse(method string, path string) {
	panic(fmt.Sprintf("a recording file system refuses %s(%s): its reader must not write", method, path))
}

func (recording *RecordingFS) UseCaseSensitiveFileNames() bool {
	return recording.underlying.UseCaseSensitiveFileNames()
}

func (recording *RecordingFS) FileExists(path string) bool {
	exists := recording.underlying.FileExists(path)
	recording.note(path, exists)
	return exists
}

func (recording *RecordingFS) ReadFile(path string) (string, bool) {
	contents, ok := recording.underlying.ReadFile(path)
	recording.note(path, ok)
	return contents, ok
}

func (recording *RecordingFS) WriteFile(path string, data string) error {
	recording.refuse("WriteFile", path)
	return nil
}

func (recording *RecordingFS) AppendFile(path string, data string) error {
	recording.refuse("AppendFile", path)
	return nil
}

func (recording *RecordingFS) Remove(path string) error {
	recording.refuse("Remove", path)
	return nil
}

func (recording *RecordingFS) Chtimes(path string, accessTime time.Time, modificationTime time.Time) error {
	recording.refuse("Chtimes", path)
	return nil
}

func (recording *RecordingFS) DirectoryExists(path string) bool {
	exists := recording.underlying.DirectoryExists(path)
	recording.note(path, exists)
	return exists
}

func (recording *RecordingFS) GetAccessibleEntries(path string) vfs.Entries {
	entries := recording.underlying.GetAccessibleEntries(path)
	recording.note(path, len(entries.Files) > 0 || len(entries.Directories) > 0 || recording.underlying.DirectoryExists(path))
	return entries
}

func (recording *RecordingFS) Stat(path string) vfs.FileInfo {
	info := recording.underlying.Stat(path)
	recording.note(path, info != nil)
	return info
}

func (recording *RecordingFS) Realpath(path string) string {
	recording.note(path, recording.underlying.FileExists(path) || recording.underlying.DirectoryExists(path))
	return recording.underlying.Realpath(path)
}
