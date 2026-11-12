package load

import (
	"io/fs"
	"time"
)

// entryInfo is a directory entry as the fs.FileInfo a build.Context's ReadDir returns: its name, whether it is a
// directory, and its type bits as its mode, which is all go/build reads of it (fs.FileInfoToDirEntry gives the type
// back from the mode). Its size and time are not read, and are zero.
type entryInfo struct {
	entry fs.DirEntry
}

func (info entryInfo) Name() string       { return info.entry.Name() }
func (info entryInfo) Size() int64        { return 0 }
func (info entryInfo) Mode() fs.FileMode  { return info.entry.Type() }
func (info entryInfo) ModTime() time.Time { return time.Time{} }
func (info entryInfo) IsDir() bool        { return info.entry.IsDir() }
func (info entryInfo) Sys() any           { return nil }
