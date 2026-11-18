package go_engine

import (
	"path/filepath"
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
)

// systemCache is the run's system cache (#syscache): a second store beside the project's, in the user's cache
// directory rather than any project's, which holds every standard-library package's entry under its key in its
// build's context, read and written as the project's store is, by every run of the same engine on the machine,
// whatever project it checks. It is found once a run, before any build reads it, and handed to every build's
// lintCache.
type systemCache struct {
	// root is the system cache's root, empty when the run keeps none.
	root string

	// store holds the standard library's entries, at `<root>/go/store`, nil when the run keeps no system cache: under
	// `--no-system-cache`, or when it has no root.
	store *cache.Store

	// off is set under `--no-system-cache`, which neither reads nor writes the system cache.
	off bool

	// notKept is why the run keeps no system cache though it was on, or could not keep it as it should, as when the
	// trim failed, nil when it kept it. The run says it and goes on: it computes what the cache would have held, and
	// its findings and exit are as without it. notKeptLock guards it.
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
	return &systemCache{root: root, store: cache.OpenAt(systemStoreDirectory(root))}
}

// systemStoreDirectory is where the system cache at root keeps its store of the standard library's entries.
func systemStoreDirectory(root string) string {
	return filepath.Join(root, "go", "store")
}

// failedAt records why the system cache is not kept as it should be when session, a build's, failed to write to it
// (check.CacheSession.SystemCacheFailure): never an error, since the next run computes what this one could not keep.
func (system *systemCache) failedAt(session *check.CacheSession) {
	if session == nil {
		return
	}
	if failure := session.SystemCacheFailure(); failure != nil {
		system.notKeptBecause(cache.NotKeptBecause(failure))
	}
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

// trim trims the system cache's store, at most once a day (cache.TrimSystemCache), once a run, after every build has
// read and written it. A trim that fails is why the system cache is not kept as it should be, never an error.
func (system *systemCache) trim() {
	if system.store == nil {
		return
	}
	if notKept := cache.TrimSystemCache(system.root, systemStoreDirectory(system.root),
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
