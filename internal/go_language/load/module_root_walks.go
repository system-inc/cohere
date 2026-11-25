package load

import (
	"path/filepath"
	"sync"
	"sync/atomic"
)

// ModuleRootWalks are one run's walks of the module roots and local replacements its loads' inputs name (#ybjbqm2):
// each build's directories are the run's walk of those roots, read once. The host's load and every extra build's
// walk the same roots, each about as long as the walk of `./...`, and a walk reads directory entries alone, their
// names and whether each holds a go.mod, which no write of the run changes, since the run adds or removes no file, so
// what one load's walk finds is every load's. A nil ModuleRootWalks shares nothing, and each load walks its roots
// again, as a test's or an editor's save does.
type ModuleRootWalks struct {
	// walked counts the walks walked, which a test reads: one for each root however many loads ask.
	walked atomic.Int64

	mutex sync.Mutex
	walks map[string]func() []string
}

// NewModuleRootWalks is walks no load has walked yet.
func NewModuleRootWalks() *ModuleRootWalks {
	return &ModuleRootWalks{walks: map[string]func() []string{}}
}

// reachable is reachableDirectories of root, walked once for the run by its cleaned path, or walked now when walks is
// nil. The directories it returns are every load's, so no caller may change them.
func (walks *ModuleRootWalks) reachable(root string) []string {
	cleaned := filepath.Clean(root)
	if walks == nil {
		return reachableDirectories(cleaned)
	}
	walks.mutex.Lock()
	walk := walks.walks[cleaned]
	if walk == nil {
		walk = sync.OnceValue(func() []string {
			walks.walked.Add(1)
			return reachableDirectories(cleaned)
		})
		walks.walks[cleaned] = walk
	}
	walks.mutex.Unlock()
	return walk()
}
