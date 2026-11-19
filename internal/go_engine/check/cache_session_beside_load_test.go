package check

import (
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
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
		goRootOf(test))), rules)
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
