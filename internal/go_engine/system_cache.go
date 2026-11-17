package go_engine

import (
	"path/filepath"
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
)

// systemCache is the run's system cache (#syscache): the standard library's tables, one for each build context, in
// the user's cache directory rather than any project's, read by every run of the same engine on the machine, whatever
// project it checks. It is found once a run, before any build reads it, and read by every build's lintCache.
type systemCache struct {
	// root is the system cache's root, empty when the run keeps none.
	root string

	// store holds the standard library's tables, at `<root>/go/standard-library`, nil when the run keeps no system
	// cache: under `--no-system-cache`, or when it has no root.
	store *cache.Store

	// off is set under `--no-system-cache`, which neither reads nor writes the system cache.
	off bool

	// notKept is why the run keeps no system cache though it was on, or could not keep it as it should, as when a
	// table's write or the trim failed, nil when it kept it. The run says it and goes on: it computes what the cache
	// would have held, and its findings and exit are as without it. notKeptLock guards it, since each extra build
	// writes its table as it ends, beside the others.
	notKept     *cache.NotKept
	notKeptLock sync.Mutex
}

// openSystemCache is the system cache request's run keeps: none under `--no-system-cache`, and otherwise the one at
// the root cache.SystemCacheRoot finds, or why it keeps none. Nothing is made or written here.
func openSystemCache(request invocation) *systemCache {
	if request.switches["no-system-cache"] {
		return &systemCache{off: true}
	}
	root := request.standardLibrary.systemCacheRoot
	if root == "" {
		found, notKept := cache.SystemCacheRoot()
		if notKept != nil {
			return &systemCache{notKept: notKept}
		}
		root = found
	}
	return &systemCache{root: root, store: cache.OpenAt(standardLibraryTablesDirectory(root))}
}

// standardLibraryTablesDirectory is where the system cache at root keeps the standard library's tables.
func standardLibraryTablesDirectory(root string) string {
	return filepath.Join(root, "go", "standard-library")
}

// write writes back session's table of its build's context, once its reads are whole, with what the build computed
// of the standard library whole beside what the table held (check.CacheSession.WriteSystemCacheTable), and nothing
// when it computed nothing new, and returns how many entries it added, for the build's record. A write that fails is
// why the system cache is not kept as it should be, never an error: the next run computes what this one could not
// keep.
func (system *systemCache) write(session *check.CacheSession) int {
	if system.store == nil || session == nil {
		return 0
	}
	added, writeError := session.WriteSystemCacheTable()
	if writeError != nil {
		system.notKeptBecause(cache.NotKeptBecause(writeError))
	}
	return added
}

// notKeptBecause records why the system cache is not kept as it should be, the first reason the run met, which the
// run says.
func (system *systemCache) notKeptBecause(notKept *cache.NotKept) {
	system.notKeptLock.Lock()
	defer system.notKeptLock.Unlock()
	if system.notKept == nil {
		system.notKept = notKept
	}
}

// trim trims the standard library's tables, at most once a day (cache.TrimSystemCache), once a run, after every
// build has read its table. A trim that fails is why the system cache is not kept as it should be, never an error.
func (system *systemCache) trim() {
	if system.store == nil {
		return
	}
	if notKept := cache.TrimSystemCache(system.root, standardLibraryTablesDirectory(system.root),
		cache.SystemCacheSizeCap); notKept != nil {
		system.notKeptBecause(notKept)
	}
}

// notKeptReason is why the run keeps no system cache, or could not keep it as it should, in one line, empty when it
// kept it or was told not to.
func (system *systemCache) notKeptReason() string {
	system.notKeptLock.Lock()
	defer system.notKeptLock.Unlock()
	if system.notKept == nil {
		return ""
	}
	return system.notKept.Reason()
}
