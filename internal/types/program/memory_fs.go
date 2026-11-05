package program

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

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

// NewMemoryFileSystem holds files, by absolute slash path, and every directory above them.
func NewMemoryFileSystem(files map[string]string) *MemoryFileSystem {
	memory := &MemoryFileSystem{
		files:       make(map[string]string, len(files)),
		directories: map[string]*vfs.Entries{"/": {}},
	}
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

// errorMemoryFileSystemReadOnly is every write's answer.
var errorMemoryFileSystemReadOnly = errors.New("program: the memory filesystem is read-only")

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

// decodeLikeDisk is typescript-go's decodeBytes, TypeScript/tsc/internal/vfs/internal/internal.go, which every
// on-disk read goes through and which the shim cannot reach: UTF-16 in either byte order behind its byte
// order mark is decoded, and a UTF-8 byte order mark is dropped. Copied rather than approximated, and held
// to the disk byte for byte by TestMemoryFSReadsBytesAsTheDiskDoes, which is where a change upstream shows.
func decodeLikeDisk(text string) string {
	if len(text) >= 2 {
		switch [2]byte{text[0], text[1]} {
		case [2]byte{0xFF, 0xFE}:
			return decodeUtf16LikeDisk(text[2:], binary.LittleEndian)
		case [2]byte{0xFE, 0xFF}:
			return decodeUtf16LikeDisk(text[2:], binary.BigEndian)
		}
	}
	if len(text) >= 3 && text[0] == 0xEF && text[1] == 0xBB && text[2] == 0xBF {
		return text[3:]
	}
	return text
}

// decodeUtf16LikeDisk is typescript-go's decodeUtf16, beside decodeBytes: a trailing odd byte is dropped, as
// binary.Read into len/2 code units leaves it unread.
func decodeUtf16LikeDisk(text string, order binary.ByteOrder) string {
	ints := make([]uint16, len(text)/2)
	if readError := binary.Read(strings.NewReader(text), order, &ints); readError != nil {
		return ""
	}
	return string(utf16.Decode(ints))
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

// memoryFileInfo is a MemoryFS entry's fs.FileInfo. Every modification time is the zero time: the bytes
// never change while a MemoryFS lives.
type memoryFileInfo struct {
	name      string
	size      int64
	directory bool
}

func (info memoryFileInfo) Name() string       { return info.name }
func (info memoryFileInfo) Size() int64        { return info.size }
func (info memoryFileInfo) ModTime() time.Time { return time.Time{} }
func (info memoryFileInfo) IsDir() bool        { return info.directory }
func (info memoryFileInfo) Sys() any           { return nil }

func (info memoryFileInfo) Mode() fs.FileMode {
	if info.directory {
		return fs.ModeDir | 0o555
	}
	return 0o444
}

var _ vfs.FS = (*MemoryFileSystem)(nil)
