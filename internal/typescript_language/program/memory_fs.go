package program

import (
	"encoding/binary"
	"errors"
	"path"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

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

var _ vfs.FS = (*MemoryFileSystem)(nil)
