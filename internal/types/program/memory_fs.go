package program

import (
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// MemoryFS is a read-only filesystem over files held in memory, for Options.FileSystem: a rule test builds
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
type MemoryFS struct {
	files       map[string]string
	directories map[string]*vfs.Entries
}

// NewMemoryFS holds files, by absolute slash path, and every directory above them.
func NewMemoryFS(files map[string]string) *MemoryFS {
	memory := &MemoryFS{files: make(map[string]string, len(files)), directories: map[string]*vfs.Entries{"/": {}}}
	for fileName, contents := range files {
		fileName = path.Clean("/" + strings.TrimPrefix(fileName, "/"))
		memory.files[fileName] = contents
		child, isFile := fileName, true
		for parent := path.Dir(child); ; parent = path.Dir(parent) {
			entries, known := memory.directories[parent]
			if !known {
				entries = &vfs.Entries{}
				memory.directories[parent] = entries
			}
			if isFile {
				entries.Files = append(entries.Files, path.Base(child))
			} else if !known || !containsName(entries.Directories, path.Base(child)) {
				entries.Directories = append(entries.Directories, path.Base(child))
			}
			if known && !isFile || parent == "/" {
				break
			}
			child, isFile = parent, false
		}
	}
	for _, entries := range memory.directories {
		sort.Strings(entries.Files)
		sort.Strings(entries.Directories)
		// No entry is a symbolic link, which an empty set says and nil would leave the compiler to check.
		entries.Symlinks = map[string]struct{}{}
	}
	return memory
}

// containsName reports whether a directory's names hold one.
func containsName(names []string, name string) bool {
	for _, existing := range names {
		if existing == name {
			return true
		}
	}
	return false
}

// errMemoryFSReadOnly is every write's answer.
var errMemoryFSReadOnly = errors.New("program: the memory filesystem is read-only")

func (m *MemoryFS) UseCaseSensitiveFileNames() bool { return true }

func (m *MemoryFS) FileExists(fileName string) bool {
	_, exists := m.files[path.Clean(fileName)]
	return exists
}

func (m *MemoryFS) ReadFile(fileName string) (string, bool) {
	contents, exists := m.files[path.Clean(fileName)]
	return contents, exists
}

func (m *MemoryFS) WriteFile(string, string) error             { return errMemoryFSReadOnly }
func (m *MemoryFS) AppendFile(string, string) error            { return errMemoryFSReadOnly }
func (m *MemoryFS) Remove(string) error                        { return errMemoryFSReadOnly }
func (m *MemoryFS) Chtimes(string, time.Time, time.Time) error { return errMemoryFSReadOnly }
func (m *MemoryFS) Realpath(fileName string) string            { return path.Clean(fileName) }

func (m *MemoryFS) DirectoryExists(directory string) bool {
	_, exists := m.directories[path.Clean(directory)]
	return exists
}

func (m *MemoryFS) GetAccessibleEntries(directory string) vfs.Entries {
	if entries, exists := m.directories[path.Clean(directory)]; exists {
		return *entries
	}
	return vfs.Entries{}
}

func (m *MemoryFS) Stat(name string) vfs.FileInfo {
	name = path.Clean(name)
	if contents, exists := m.files[name]; exists {
		return memoryFileInfo{name: path.Base(name), size: int64(len(contents))}
	}
	if _, exists := m.directories[name]; exists {
		return memoryFileInfo{name: path.Base(name), directory: true}
	}
	return nil
}

// memoryFileInfo is a MemoryFS entry's fs.FileInfo. Every modification time is the zero time: the bytes
// never change while a MemoryFS lives.
type memoryFileInfo struct {
	name      string
	size      int64
	directory bool
}

func (i memoryFileInfo) Name() string       { return i.name }
func (i memoryFileInfo) Size() int64        { return i.size }
func (i memoryFileInfo) ModTime() time.Time { return time.Time{} }
func (i memoryFileInfo) IsDir() bool        { return i.directory }
func (i memoryFileInfo) Sys() any           { return nil }

func (i memoryFileInfo) Mode() fs.FileMode {
	if i.directory {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

var _ vfs.FS = (*MemoryFS)(nil)
