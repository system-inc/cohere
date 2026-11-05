package program

import (
	"io/fs"
	"time"
)

// memoryFileInfo is a MemoryFS entry's fs.FileInfo. Every modification time is the zero time: the bytes
// never change while a MemoryFS lives.
type memoryFileInfo struct {
	name      string
	size      int64
	directory bool
}

func (info memoryFileInfo) Name() string { return info.name }

func (info memoryFileInfo) Size() int64 { return info.size }

func (info memoryFileInfo) ModTime() time.Time { return time.Time{} }

func (info memoryFileInfo) IsDir() bool { return info.directory }

func (info memoryFileInfo) Sys() any { return nil }

func (info memoryFileInfo) Mode() fs.FileMode {
	if info.directory {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
