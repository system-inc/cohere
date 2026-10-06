package program

import (
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/vfs"
)

// readIdentities is the stat each file's bytes were read under, for a program built without a content pack,
// which keeps its own (#q6dey77). It lets a reader holding the program's copy of a file ask whether that copy
// is still the file's without reading it again. In memory only: a `--no-cache` run reads and writes no cache.
type readIdentities struct {
	mutex  sync.Mutex
	byPath map[string]fileIdentity
}

// identityFS stats each file before reading it, as the content pack does, so the identity it records can only
// be older than the bytes read after it, and records it when both succeed. Everything else passes through.
type identityFS struct {
	vfs.FS
	identities *readIdentities
}

func (f *identityFS) ReadFile(path string) (string, bool) {
	identity, statted := statIdentity(path)
	contents, ok := f.FS.ReadFile(path)
	if statted && ok {
		f.identities.mutex.Lock()
		f.identities.byPath[path] = identity
		f.identities.mutex.Unlock()
	}
	return contents, ok
}

// unchanged reports whether path has a recorded identity that a stat taken now still matches.
func (r *readIdentities) unchanged(path string) bool {
	if r == nil {
		return false
	}
	r.mutex.Lock()
	identity, known := r.byPath[path]
	r.mutex.Unlock()
	if !known {
		return false
	}
	current, statted := statIdentity(path)
	return statted && current == identity
}
