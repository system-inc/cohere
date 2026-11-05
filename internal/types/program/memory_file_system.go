package program

import (
	"path"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// MemoryFileSystem is a read-only filesystem over files held in memory, for Options.FileSystem: a rule test builds
// its fixture program from a map rather than from files it writes to a temporary directory and deletes.
//
// The rule-test harness wrote about 16,000 fixture directories to /tmp for one registry run, and deleting
// them at exit was 3.7s of its 7.6s: the filesystem serializes the unlinks, so deleting them in parallel was
// slower (#nxgt2ca). A program reads its tree through vfs.FS and nothing else, so a map serves it the same
// bytes and costs no syscall.
//
// typescript-go has a map-backed test filesystem of its own, vfstest.FromMap, but it is generic, and a
// generic function cannot cross the shim's linkname. This is the read half of it, which is all a build
// asks of a filesystem: paths are case-sensitive and slash-separated, nothing is a symbolic link, and every
// write fails, since a build that writes (an incremental session's build info) has nowhere to write to.
//
// Safe for concurrent use: it is never written after NewMemoryFS returns.
type MemoryFileSystem struct {
	files       map[string]string
	directories map[string]*vfs.Entries
}

func (fileSystem *MemoryFileSystem) CaseSensitivity() tspath.CaseSensitivity {
	return tspath.CaseSensitive
}

func (fileSystem *MemoryFileSystem) FileExists(fileName tspath.RootedFilePath) bool {
	_, exists := fileSystem.files[path.Clean(fileName.AsString())]
	return exists
}

// ReadFile answers as the disk does: a file's bytes decoded the way the real filesystem decodes them, so a
// fixture's byte order mark or UTF-16 reaches the compiler as it would from a file.
func (fileSystem *MemoryFileSystem) ReadFile(fileName tspath.RootedFilePath) (string, bool) {
	contents, exists := fileSystem.files[path.Clean(fileName.AsString())]
	if !exists {
		return "", false
	}
	return decodeLikeDisk(contents), true
}

func (fileSystem *MemoryFileSystem) WriteFile(tspath.RootedFilePath, string) error {
	return errorMemoryFileSystemReadOnly
}

func (fileSystem *MemoryFileSystem) AppendFile(tspath.RootedFilePath, string) error {
	return errorMemoryFileSystemReadOnly
}

func (fileSystem *MemoryFileSystem) Remove(tspath.RootedPath) error {
	return errorMemoryFileSystemReadOnly
}

func (fileSystem *MemoryFileSystem) Chtimes(tspath.RootedPath, time.Time, time.Time) error {
	return errorMemoryFileSystemReadOnly
}

func (fileSystem *MemoryFileSystem) Realpath(fileName tspath.RootedPath) tspath.RootedPath {
	return tspath.RootedPath(path.Clean(fileName.AsString()))
}

func (fileSystem *MemoryFileSystem) DirectoryExists(directory tspath.RootedDirectoryPath) bool {
	_, exists := fileSystem.directories[path.Clean(directory.AsString())]
	return exists
}

func (fileSystem *MemoryFileSystem) GetAccessibleEntries(directory tspath.RootedDirectoryPath) vfs.Entries {
	if entries, exists := fileSystem.directories[path.Clean(directory.AsString())]; exists {
		return *entries
	}
	return vfs.Entries{}
}

func (fileSystem *MemoryFileSystem) Stat(rooted tspath.RootedPath) vfs.FileInfo {
	name := path.Clean(rooted.AsString())
	if contents, exists := fileSystem.files[name]; exists {
		return memoryFileInfo{name: path.Base(name), size: int64(len(contents))}
	}
	if _, exists := fileSystem.directories[name]; exists {
		return memoryFileInfo{name: path.Base(name), directory: true}
	}
	return nil
}
