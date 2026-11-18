package check

import (
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// The host computes the dependence set of every package it kept as the package's walk ends, so an extra build's walk
// computes none and finds each in the memo (#ybjbqm2): the host's run over the write-through fixture, its walks and
// its sets waited for (HostRun.computeDependence), has its memo filled, and windows, which restores both of its
// packages from the host, asks the memo the set of each and computes none.
// Computed in the walk instead, each set cost seven builds racing to compute it, 223 ms of each build's walk at
// cohere's root.
func TestAnExtraBuildsWalkFindsEveryDependenceSetTheHostComputed(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	host := throughHost(test, root, test.TempDir())
	// The sets are computed beside the run, so the test waits for them before it counts the build's.
	host.computing.Wait()
	held := 0
	host.renderings.dependences.Range(func(_, _ any) bool {
		held++
		return true
	})
	if held == 0 {
		test.Errorf("the host computed no dependence set of the %d packages it kept", host.EntriesKept())
	}
	// The build's verdicts run on several goroutines, so what the hook is told is gathered under a lock.
	var lock sync.Mutex
	var computed []string
	host.renderings.computing = func(restoring Restoring) {
		lock.Lock()
		defer lock.Unlock()
		computed = append(computed, restoring.Path)
	}
	result, _ := throughBuild(test, root, test.TempDir(), host)
	fromHost := slices.Concat(result.FromHost.Restored, result.FromHost.Untouched)
	if !slices.Contains(fromHost, throughBasePath) || !slices.Contains(fromHost, throughUserPath) {
		test.Fatalf("windows restored %q from the host, want base and user, whose sets its walk asks", fromHost)
	}
	if len(computed) > 0 {
		test.Errorf("windows's walk computed the dependence sets of %q, want each found in the memo the host filled",
			computed)
	}
}

// The host's keep runs inside Run, while the type-read walks may still be running, so the host computes each kept
// package's set only once that package's own walk has ended, and no other (#ybjbqm2): with no store, as under
// `--no-cache`, whose run never waits for its walks, user's walk is held past Run's return while the host asks the
// set of every other package it kept, and of user only once its walk has ended. Once the walks and the host's sets
// are waited for, the memo holds user's set as the packages it reads, computed by the host, never the whole closure
// its reads gave while still pending. Waiting for every walk instead holds the others' sets until the test times out.
func TestTheHostComputesEachSetOnceItsPackagesWalkEnds(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	rules := throughRules()
	caching := cachingAt(test.TempDir(), rules)
	caching.Store = nil
	session := NewCacheSession(caching, rules)
	session.KeepForBuilds()
	loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, RecordReads: load.EveryPackage})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the host's build: %v %+v", loadError, loaded.Diagnostics)
	}
	// Each package the host kept but user is told on others, which holds one for every package the load reached, so
	// the pass never blocks on it.
	reached := 0
	packages.Visit(loaded.Packages, nil, func(*packages.Package) { reached++ })
	others := make(chan *packages.Package, reached)
	var lock sync.Mutex
	askedPending := false
	keptDependence := func(owner *packages.Package, restoring Restoring) {
		if owner.PkgPath != throughUserPath {
			others <- owner
			return
		}
		lock.Lock()
		defer lock.Unlock()
		askedPending = askedPending || !restoring.Reads.Recorded()
	}
	hold := make(chan struct{})
	var released sync.Once
	release := func() { released.Do(func() { close(hold) }) }
	// A failure below still lets the walk go, so the walks the test leaves behind end.
	defer release()
	result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld,
		Bodiless: loaded.Bodiless, Reads: loaded.Reads, WalkSlots: loaded.WalkSlots, KeepForBuilds: true,
		holdWalk: func(owner *packages.Package) {
			if owner.PkgPath == throughUserPath {
				<-hold
			}
		}, KeptDependence: keptDependence})
	if runError != nil || len(result.Crashes) > 0 {
		test.Fatalf("running the host's build: %v %+v", runError, result.Crashes)
	}
	user := result.Host.entries[throughUserPath]
	if user == nil {
		test.Fatalf("the host kept no entry of user, so nothing asks its set")
	}
	if user.restoring.Reads.Recorded() {
		test.Fatalf("user's reads were recorded while its walk was held")
	}
	for range result.Host.EntriesKept() - 1 {
		<-others
	}
	release()
	result.ReadWalks.Wait()
	result.Host.computing.Wait()
	lock.Lock()
	if askedPending {
		test.Errorf("the host asked user's set while its walk was held, its reads still pending")
	}
	lock.Unlock()
	known, stored := result.Host.renderings.dependences.Load(user.restoring.Reads)
	if !stored {
		test.Fatalf("the host computed no set of user once its walk ended")
	}
	if set := knownDependence(user.restoring, known); set.WholeClosure || !set.Paths[throughBasePath] {
		test.Errorf("user's set, as the host computed it, is %+v, want the packages it reads, base among them", set)
	}
}
