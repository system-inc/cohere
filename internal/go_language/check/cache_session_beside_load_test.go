package check

import (
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/testing/go_root"
)

// A run beside the load saves a standard-library package to the system cache only once the load has made it final
// (#641hyrh). With nothing to analyze, as a run whose census counted no file has, no action waits on any package, and
// the save once asked about every package the load reached while the load was still checking some: the race detector
// caught savable reading a package's IllTyped as the load's check wrote it. Here the load is held on unicode/utf8, its
// readiness open while the test writes what the load's check writes, so under `-race` a save that reads it before the
// readiness closes is a race every time. The run then writes every standard-library package, the held one too: the
// save waits for it rather than passing it by.
func TestARunBesideTheLoadSavesAPackageOnlyOnceTheLoadHasMadeItFinal(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	session := NewCacheSession(systemCacheCaching(systemStore(test), standardLibraryContext("check tests",
		go_root.GoRoot(test))), rules)
	loaded, loadError := load.PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session})
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	var held *packages.Package
	packages.Visit(loaded.Packages, nil, func(visited *packages.Package) {
		if visited.PkgPath == "unicode/utf8" {
			held = visited
		}
	})
	if held == nil {
		test.Fatal("want unicode/utf8 among the packages loaded")
	}
	if _, keyed := session.keys[held]; !keyed {
		test.Fatal("want unicode/utf8 keyed for the system cache, so the save asks about it")
	}
	holding := make(chan struct{})
	final := make(chan struct{})
	close(final)
	ready := func(asked *packages.Package) <-chan struct{} {
		if asked == held {
			return holding
		}
		return final
	}
	// The run's outcome, read only once finished is closed.
	var result *Result
	var runError error
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result, runError = Run(nil, rules, Options{Cache: session, Ready: ready, Abandon: make(chan struct{})})
	}()
	// What the load's check of the held package writes last (loader.check), written again while it is held, and only
	// then is it final.
	held.IllTyped = len(held.Errors) > 0
	close(holding)
	<-finished
	if runError != nil {
		test.Fatalf("running beside the load: %v", runError)
	}
	if written := result.Cache.SystemCacheWritten; written == 0 || written != len(session.standardLibraryKeys) {
		test.Errorf("want every standard-library package written to the system cache, the held one too, got %d of "+
			"%d", written, len(session.standardLibraryKeys))
	}
}

// A run beside the load keeps a package for the extra builds only once the load has made it final (#641hyrh). The keep
// reads every package the load reached, the project's too, which the system cache never keys, so the save never waits
// on them. With nothing to analyze, no action waits on any package, and the keep reads a package's errors and IllTyped
// as the load's check may still write them. Here the load is held on the module's own package, kept for the builds and
// not keyed for the system cache, its readiness open while the test writes what the load's check writes, and held until
// the run either asks its readiness before the keep or returns: a run that returns while it is held kept it before it
// was final.
func TestARunBesideTheLoadKeepsAPackageOnlyOnceTheLoadHasMadeItFinal(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	session := NewCacheSession(systemCacheCaching(systemStore(test), standardLibraryContext("check tests",
		go_root.GoRoot(test))), rules)
	// The host's session of a run with extra builds, which keys every package it loads for the keep (keptKeys).
	session.KeepForBuilds()
	loaded, loadError := load.PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session, RecordReads: load.EveryPackage})
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	var held *packages.Package
	packages.Visit(loaded.Packages, nil, func(visited *packages.Package) {
		if visited.PkgPath == "example.com/standard" {
			held = visited
		}
	})
	if held == nil {
		test.Fatal("want example.com/standard among the packages loaded")
	}
	if session.keptKeys == nil {
		test.Fatal("want the load keyed for the keep, so the keep reads the held package")
	}
	if _, kept := session.keptKeys.Keys[held]; !kept {
		test.Fatal("want example.com/standard keyed for the keep")
	}
	if _, keyed := session.keys[held]; keyed {
		test.Fatal("want example.com/standard keyed for the keep alone, so only the keep's wait holds it")
	}
	// The run asks the held package's readiness for the walk of its type reads (recordUnreachedTypes), when nothing
	// has claimed them, and then for the wait before the keep (awaitLoaded), whose ask closes waiting. The test holds
	// the package until then, so the walk, which takes the package's reads' lock once it is ready, as the keep does,
	// orders nothing before the keep's read.
	asks := int32(1)
	if !loaded.Reads[held].Claimed() {
		asks = 2
	}
	var asked atomic.Int32
	waiting := make(chan struct{})
	holding := make(chan struct{})
	final := make(chan struct{})
	close(final)
	ready := func(readied *packages.Package) <-chan struct{} {
		if readied != held {
			return final
		}
		if asked.Add(1) == asks {
			close(waiting)
		}
		return holding
	}
	// The run's outcome, read only once finished is closed.
	var result *Result
	var runError error
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result, runError = Run(nil, rules, Options{Cache: session, Ready: ready, Abandon: make(chan struct{}),
			Withheld: loaded.Withheld, Bodiless: loaded.Bodiless, Reads: loaded.Reads, WalkSlots: loaded.WalkSlots,
			KeepForBuilds: true})
	}()
	// What the load's check of the held package writes last (loader.check), written again while it is held, and only
	// then is it final.
	held.IllTyped = len(held.Errors) > 0
	select {
	case <-waiting:
	case <-finished:
		test.Error("want the run beside the load to wait for the held package before it keeps, and it returned")
	}
	close(holding)
	<-finished
	if runError != nil {
		test.Fatalf("running beside the load: %v", runError)
	}
	// The walks and the dependence sets the run leaves running read the packages too, so the test ends only with them.
	result.ReadWalks.Wait()
	if result.Host == nil {
		test.Fatal("want the run to keep its packages for the extra builds")
	}
	result.Host.DependenceComputed()
}
