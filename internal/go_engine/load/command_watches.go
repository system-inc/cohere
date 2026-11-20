package load

import (
	"sync"
	"sync/atomic"
)

// commandWatches are, by workspace, the counts of one go command's runs a test watches, under lock. Empty but in a
// test.
type commandWatches struct {
	lock        sync.Mutex
	byWorkspace map[string]*atomic.Int64
}

// goEnvironmentWatches count go env's asks (WatchGoEnvironmentAsks).
var goEnvironmentWatches = commandWatches{byWorkspace: map[string]*atomic.Int64{}}

// WatchGoEnvironmentAsks counts every go env askGoEnvironment runs in workspace, absolute, until stop is called, for a
// test holding a whole run to one ask per build. Only a test calls it; it is exported for the engine's tests, which
// run the whole pipeline over a fixture, and keyed by workspace, since tests in one process run many workspaces at
// once.
func WatchGoEnvironmentAsks(workspace string) (asks *atomic.Int64, stop func()) {
	return goEnvironmentWatches.watch(workspace)
}

// listerCommandWatches count the runs of each of listerCommands a listerState starts, by its index (WatchBuildLists,
// WatchBuildContexts).
var listerCommandWatches = [len(listerCommands)]commandWatches{
	{byWorkspace: map[string]*atomic.Int64{}},
	{byWorkspace: map[string]*atomic.Int64{}},
}

// WatchBuildLists counts every `go list -m -json all` the lister's state runs in workspace, absolute, until stop is
// called, for a test holding a run to one build list for every build that shares the host's (ShareModulesOf). Only a
// test calls it; it is exported for the engine's tests, as WatchGoEnvironmentAsks is.
func WatchBuildLists(workspace string) (lists *atomic.Int64, stop func()) {
	return listerCommandWatches[buildList].watch(workspace)
}

// WatchBuildContexts counts every go list of the build context (contextTemplate) the lister's state runs in
// workspace, absolute, until stop is called, for a test holding a run to none where the system cache keeps every
// build's (KeepContextIn). Only a test calls it, as WatchBuildLists.
func WatchBuildContexts(workspace string) (contexts *atomic.Int64, stop func()) {
	return listerCommandWatches[buildContext].watch(workspace)
}

// watch counts each run in workspace, absolute, until stop is called.
func (watches *commandWatches) watch(workspace string) (runs *atomic.Int64, stop func()) {
	runs = &atomic.Int64{}
	watches.lock.Lock()
	defer watches.lock.Unlock()
	watches.byWorkspace[workspace] = runs
	return runs, func() {
		watches.lock.Lock()
		defer watches.lock.Unlock()
		delete(watches.byWorkspace, workspace)
	}
}

// counted adds one run in workspace to the count a test watches there, if any.
func (watches *commandWatches) counted(workspace string) {
	watches.lock.Lock()
	defer watches.lock.Unlock()
	if runs := watches.byWorkspace[workspace]; runs != nil {
		runs.Add(1)
	}
}
