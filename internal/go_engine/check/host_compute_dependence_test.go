package check

import (
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
)

// The host computes the dependence set of every package it kept once its reads are whole, before any build starts, so
// an extra build's walk computes none and finds each in the memo (#ybjbqm2): the host's run over the write-through
// fixture, its walks waited for, has its memo filled as the engine has it filled (HostRun.ComputeDependence), and
// windows, which restores both of its packages from the host, asks the memo the set of each and computes none.
// Computed in the walk instead, each set cost seven builds racing to compute it, 223 ms of each build's walk at
// cohere's root.
func TestAnExtraBuildsWalkFindsEveryDependenceSetTheHostComputed(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	host := throughHost(test, root, test.TempDir())
	host.ComputeDependence(nil)
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

// The host's keep runs inside Run, while the type-read walks may still be running, so the host computes no set until
// they are waited for (#ybjbqm2): with no store, as under `--no-cache`, whose run never waits for its walks, user's
// walk is held past Run's return, the keep has computed no set, and once the walk is let go and waited for, the sets
// the host computes give user the packages it reads, never the whole closure its reads would give while pending.
func TestTheHostComputesNoDependenceSetBeforeItsReadsAreWhole(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	rules := throughRules()
	caching := cachingAt(test.TempDir(), rules)
	caching.Store = nil
	session := NewCacheSession(caching, rules)
	session.KeepForBuilds()
	loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, RecordReads: true})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading the host's build: %v %+v", loadError, loaded.Diagnostics)
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
		}})
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
	held := 0
	result.Host.renderings.dependences.Range(func(_, _ any) bool {
		held++
		return true
	})
	if held > 0 {
		test.Errorf("the host's keep computed %d dependence sets while user's walk was held, want none before the "+
			"walks are waited for", held)
	}
	release()
	result.ReadWalks.Wait()
	result.Host.ComputeDependence(nil)
	set := result.Host.renderings.Dependence(user.restoring)
	if set.WholeClosure || !set.Paths[throughBasePath] {
		test.Errorf("user's set, its reads whole, is %+v, want the packages it reads, base among them", set)
	}
}
