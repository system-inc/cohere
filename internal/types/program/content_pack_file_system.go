package program

import (
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// contentPackFileSystem answers ReadFile from the pack when the file's stat still matches, and otherwise reads the
// disk and hands the bytes to the pack. Everything else passes through.
type contentPackFileSystem struct {
	vfs.FS
	pack *ContentPack
}

func (fileSystem *contentPackFileSystem) ReadFile(path tspath.RootedFilePath) (string, bool) {
	if contents, served := fileSystem.pack.serve(path.AsString()); served {
		return contents, true
	}
	// Stat first, so the key can only be older than the bytes read after it.
	identity, statted := statIdentity(path.AsString())
	contents, ok := fileSystem.FS.ReadFile(path)
	fileSystem.pack.noteRead(path.AsString(), identity, statted && ok, contents)
	return contents, ok
}
