package program

import (
	"sort"
	"sync"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// InputRecorder notes every path the compiler read or looked for on disk during a build.
//
// It exists for the run cache, and the reason it observes rather than enumerates is that every
// hand-written list of a build's inputs turned out to be missing something. The program's source files
// are the obvious inputs. Less obvious: the tsconfig's extends chain, every package.json module
// resolution consulted, and every path resolution probed and did NOT find, since creating one of those
// changes what an import resolves to. An in-place edit to a package.json beside the tsconfig moves no
// directory's mtime, so a cache that only watched program files and their directories would replay a
// stale verdict over a changed resolution. Recording at the filesystem boundary sees all of them
// because it sees whatever the compiler did, not what someone remembered it does.
//
// It wraps the real disk beneath the memoizing layer, so each path reaches it about once, and beneath
// the embedded lib overlay, so the bundled lib.*.d.ts files never appear here: they are part of the
// binary, and the binary is already in the run cache's key.
type InputRecorder struct {
	mutex sync.Mutex

	// existed maps each path the build touched to whether it was there. A path seen both ways keeps
	// true: something the run read is an input that must keep its signature.
	existed map[string]bool

	// written is every path the build wrote, which is an output rather than an input. The types phase
	// reads its incremental build info at the start and writes it at the end; recorded as an input it
	// would change on every run, and every following check would miss while the cache looked merely
	// cold. Dropping it is sound because the incremental path already guarantees identical findings
	// warm or cold, so its contents cannot change the verdict being cached.
	written map[string]bool
}

// NewInputRecorder returns an empty recorder.
func NewInputRecorder() *InputRecorder {
	return &InputRecorder{existed: map[string]bool{}, written: map[string]bool{}}
}

func (r *InputRecorder) note(path string, present bool) {
	if r == nil || path == "" {
		return
	}
	r.mutex.Lock()
	r.existed[path] = r.existed[path] || present
	r.mutex.Unlock()
}

// Inputs reports what the build depended on: paths that existed, and paths it looked for and did not
// find. Anything the build itself wrote is excluded. Sorted so two recordings of one tree compare equal.
func (r *InputRecorder) Inputs() (present []string, absent []string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for path, existed := range r.existed {
		if r.written[path] {
			continue
		}
		if existed {
			present = append(present, path)
		} else {
			absent = append(absent, path)
		}
	}
	sort.Strings(present)
	sort.Strings(absent)
	return present, absent
}

// recordingFS is a vfs.FS that reports each path it serves to an InputRecorder.
type recordingFS struct {
	vfs.FS
	recorder *InputRecorder
}

// noteAnswer records a path the compiler asked about, by what is on disk rather than by the answer.
//
// A negative answer to a typed question is not absence. Module resolution asks whether
// `./Graph.css` is a directory, gets no, and moves on, and the file is right there. Recording that
// "no" as "absent" made the next check find the file and report "now exists" on an untouched tree,
// four-month-old file, so the cache never hit. FileExists on a directory has the same shape. So a
// negative answer is followed by a stat: anything on disk in any form is recorded as present, with
// its full signature, which also catches a file turning into a directory. Only a path that is
// genuinely not there is recorded absent.
func (f *recordingFS) noteAnswer(path string, positive bool) {
	if !positive {
		positive = f.FS.Stat(path) != nil
	}
	f.recorder.note(path, positive)
}

func (f *recordingFS) FileExists(path string) bool {
	exists := f.FS.FileExists(path)
	f.noteAnswer(path, exists)
	return exists
}

func (f *recordingFS) ReadFile(path string) (string, bool) {
	contents, ok := f.FS.ReadFile(path)
	f.noteAnswer(path, ok)
	return contents, ok
}

func (f *recordingFS) DirectoryExists(path string) bool {
	exists := f.FS.DirectoryExists(path)
	f.noteAnswer(path, exists)
	return exists
}

// GetAccessibleEntries is a directory listing, and a listing's answer changes when an entry is added.
// Recording the directory means its mtime is watched, which is what catches a file added beside the
// ones the build read.
func (f *recordingFS) GetAccessibleEntries(path string) vfs.Entries {
	entries := f.FS.GetAccessibleEntries(path)
	f.noteAnswer(path, f.FS.DirectoryExists(path))
	return entries
}

func (f *recordingFS) Stat(path string) vfs.FileInfo {
	information := f.FS.Stat(path)
	f.noteAnswer(path, information != nil)
	return information
}

func (f *recordingFS) WalkDir(root string, walkFn vfs.WalkDirFunc) error {
	f.noteAnswer(root, f.FS.DirectoryExists(root))
	return f.FS.WalkDir(root, walkFn)
}

func (f *recordingFS) Realpath(path string) string {
	resolved := f.FS.Realpath(path)
	// Realpath answers for a missing path too, by returning it unchanged, so existence is asked
	// rather than assumed. A probe recorded as present that is not would fail the record outright.
	f.noteAnswer(path, false)
	return resolved
}

func (f *recordingFS) WriteFile(path string, data string) error {
	f.markWritten(path)
	return f.FS.WriteFile(path, data)
}

func (f *recordingFS) AppendFile(path string, data string) error {
	f.markWritten(path)
	return f.FS.AppendFile(path, data)
}

func (f *recordingFS) Remove(path string) error {
	f.markWritten(path)
	return f.FS.Remove(path)
}

func (f *recordingFS) Chtimes(path string, accessTime time.Time, modifiedTime time.Time) error {
	f.markWritten(path)
	return f.FS.Chtimes(path, accessTime, modifiedTime)
}

func (f *recordingFS) markWritten(path string) {
	f.recorder.mutex.Lock()
	f.recorder.written[path] = true
	f.recorder.mutex.Unlock()
}
