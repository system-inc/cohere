package load

import (
	"sync"
	"sync/atomic"

	"github.com/system-inc/cohere/internal/testing/listed_directories"
)

// PackageWalks is the package directories one run's loads share, the host's and each extra build's (#tb538na,
// #vnb7btx): each pattern's walk of a root (listed_directories.PackageDirectories), walked once for every load. A walk
// reads directory entries, their names and whether each holds a go.mod, and wanted decides by path alone, so no
// build's target, tags or cgo changes what it finds. Every extra build walked what the first had just walked, about
// 0.3 CPU-s a run at cohere's root, and the first walked what the host's load had, each build waiting about 45 ms of
// its listing there for the one walk.
//
// The builds take the walks read since the run last changed the bytes it holds: the run starts its PackageWalks over
// at every such change (the pipeline's heldChanged), as it does its Fingerprints, so a walk a load made before the
// fix, the wrap or a rename wrote is never a later load's. The host's last load is after the run's last write on
// every path but a rename's, whose load of what it wrote shares nothing, so there the builds walk among themselves,
// as they do after a host that listed nothing, its listing kept, and so walked nothing.
//
// That is the stricter of two reasons, each enough. The other is the run's one invariant, said with the pipeline's
// reads: a walk reads directory names and the presence of a go.mod alone, and no write of a run leaves either made or
// removed outside a directory whose name starts with a dot, which no walk enters. A nil PackageWalks shares nothing,
// and each walk is walked again.
type PackageWalks struct {
	// computed counts the walks walked and asked the walks asked for, which a test reads: one walked for each root and
	// pattern however many loads ask.
	computed atomic.Int64
	asked    atomic.Int64

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

// NewPackageWalks is walks no load has walked yet.
func NewPackageWalks() *PackageWalks {
	return &PackageWalks{walks: map[packageWalkKey]*packageWalk{}}
}

// Walked is how many patterns' walks the walks hold, how many times one was walked and how many times one was asked
// for, all zero for nil: a pattern asked for more often than it was walked was asked by a second load, which took the
// first's walk.
func (walks *PackageWalks) Walked() (int, int64, int64) {
	if walks == nil {
		return 0, 0, 0
	}
	walks.mutex.Lock()
	defer walks.mutex.Unlock()
	return len(walks.walks), walks.computed.Load(), walks.asked.Load()
}

// directories is listed_directories.PackageDirectories of key's root under wanted, walked once for the run, or walked
// now when walks is nil. wanted must be the filter key's pattern names, since only the first load's is walked. The
// directories it returns are every load's, so no caller may change them.
func (walks *PackageWalks) directories(key packageWalkKey, wanted func(string) bool) ([]string, string) {
	if walks == nil {
		return listed_directories.PackageDirectories(key.root, wanted)
	}
	walks.asked.Add(1)
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
