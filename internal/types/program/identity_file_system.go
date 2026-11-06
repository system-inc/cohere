package program

import (
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// identityFileSystem stats each file before reading it, as the content pack does, so the identity it records can only
// be older than the bytes read after it, and records it when both succeed. Everything else passes through.
type identityFileSystem struct {
	vfs.FS
	identities *readIdentities
}

func (fileSystem *identityFileSystem) ReadFile(path tspath.RootedFilePath) (string, bool) {
	identity, statted := statIdentity(path.AsString())
	contents, ok := fileSystem.FS.ReadFile(path)
	if statted && ok {
		fileSystem.identities.mutex.Lock()
		fileSystem.identities.byPath[path.AsString()] = identity
		fileSystem.identities.mutex.Unlock()
	}
	return contents, ok
}
