package load

import (
	"sync"
	"sync/atomic"

	"github.com/system-inc/cohere/internal/listed_directories"
)

// PackageWalks is the package directories one run's extra builds share (#tb538na): each pattern's walk of a root
// (listed_directories.PackageDirectories), walked once for every build. A walk reads directory entries, their names
// and whether each holds a go.mod, and wanted decides by path alone, so no build's target, tags or cgo changes what
// it finds, and every extra build walked what the first had just walked, about 0.3 CPU-s a run at cohere's root.
//
// It is shared by loads after which nothing writes, which the extra builds' are and the host's first load is not, as
// Fingerprints is: the long-line wrap writes between that load and the builds. A nil PackageWalks shares nothing, and
// each walk is walked again.
type PackageWalks struct {
	// computed counts the walks walked, which a test reads: one for each root and pattern however many builds ask.
	computed atomic.Int64

	mutex sync.Mutex
	walks map[packageWalkKey]*packageWalk
}

// packageWalkKey is what decides a walk: its root, and the pattern that filters it, `./...` for a local walk of
// every directory, and a module pattern's prefix with the module's path, whose import paths its filter reads.
type packageWalkKey struct {
	root       string
	modulePath string
	pattern    string
}

// packageWalk is one walk, walked once.
type packageWalk struct {
	once        sync.Once
	directories []string
	doubt       string
}

// NewPackageWalks is walks no build has walked yet.
func NewPackageWalks() *PackageWalks {
	return &PackageWalks{walks: map[packageWalkKey]*packageWalk{}}
}

// directories is listed_directories.PackageDirectories of key's root under wanted, walked once for the run, or walked
// now when walks is nil. wanted must be the filter key's pattern names, since only the first build's is walked. The
// directories it returns are every build's, so no caller may change them.
func (walks *PackageWalks) directories(key packageWalkKey, wanted func(string) bool) ([]string, string) {
	if walks == nil {
		return listed_directories.PackageDirectories(key.root, wanted)
	}
	walks.mutex.Lock()
	walk := walks.walks[key]
	if walk == nil {
		walk = &packageWalk{}
		walks.walks[key] = walk
	}
	walks.mutex.Unlock()
	walk.once.Do(func() {
		walks.computed.Add(1)
		walk.directories, walk.doubt = listed_directories.PackageDirectories(key.root, wanted)
	})
	return walk.directories, walk.doubt
}
