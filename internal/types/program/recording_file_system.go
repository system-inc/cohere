package program

import (
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// recordingFileSystem is a vfs.FS that reports each path it serves to an InputRecorder.
type recordingFileSystem struct {
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
//
// depended says whether the answer depended on more than existence: a read, a listing or a stat.
func (fileSystem *recordingFileSystem) noteAnswer(path string, positive bool, depended bool) {
	present := positive
	if !positive {
		present = fileSystem.FS.Stat(tspath.RootedPath(path)) != nil
	}
	fileSystem.recorder.note(path, present, depended)
}

func (fileSystem *recordingFileSystem) FileExists(path tspath.RootedFilePath) bool {
	exists := fileSystem.FS.FileExists(path)
	fileSystem.noteAnswer(path.AsString(), exists, false)
	return exists
}

func (fileSystem *recordingFileSystem) ReadFile(path tspath.RootedFilePath) (string, bool) {
	contents, ok := fileSystem.FS.ReadFile(path)
	fileSystem.noteAnswer(path.AsString(), ok, true)
	return contents, ok
}

func (fileSystem *recordingFileSystem) DirectoryExists(path tspath.RootedDirectoryPath) bool {
	exists := fileSystem.FS.DirectoryExists(path)
	fileSystem.noteAnswer(path.AsString(), exists, false)
	return exists
}

// GetAccessibleEntries is a directory listing, and a listing's answer changes when an entry is added.
// Recording the directory means its mtime is watched, which is what catches a file added beside the
// ones the build read.
func (fileSystem *recordingFileSystem) GetAccessibleEntries(path tspath.RootedDirectoryPath) vfs.Entries {
	entries := fileSystem.FS.GetAccessibleEntries(path)
	fileSystem.noteAnswer(path.AsString(), fileSystem.FS.DirectoryExists(path), true)
	return entries
}

func (fileSystem *recordingFileSystem) Stat(path tspath.RootedPath) vfs.FileInfo {
	information := fileSystem.FS.Stat(path)
	fileSystem.noteAnswer(path.AsString(), information != nil, true)
	return information
}

func (fileSystem *recordingFileSystem) Realpath(path tspath.RootedPath) tspath.RootedPath {
	resolved := fileSystem.FS.Realpath(path)
	// Realpath answers for a missing path too, by returning it unchanged, so existence is asked
	// rather than assumed. A probe recorded as present that is not would fail the record outright.
	fileSystem.noteAnswer(path.AsString(), false, false)
	return resolved
}

func (fileSystem *recordingFileSystem) WriteFile(path tspath.RootedFilePath, data string) error {
	fileSystem.markWritten(path.AsString())
	return fileSystem.FS.WriteFile(path, data)
}

func (fileSystem *recordingFileSystem) AppendFile(path tspath.RootedFilePath, data string) error {
	fileSystem.markWritten(path.AsString())
	return fileSystem.FS.AppendFile(path, data)
}

func (fileSystem *recordingFileSystem) Remove(path tspath.RootedPath) error {
	fileSystem.markWritten(path.AsString())
	return fileSystem.FS.Remove(path)
}

func (fileSystem *recordingFileSystem) Chtimes(
	path tspath.RootedPath,
	accessTime time.Time,
	modifiedTime time.Time,
) error {
	fileSystem.markWritten(path.AsString())
	return fileSystem.FS.Chtimes(path, accessTime, modifiedTime)
}

func (fileSystem *recordingFileSystem) markWritten(path string) {
	trimmed := withoutTrailingSlash(path)
	fileSystem.recorder.mutex.Lock()
	fileSystem.recorder.written[trimmed] = true
	fileSystem.recorder.mutex.Unlock()
}
