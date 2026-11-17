package check

import (
	"slices"
	"sync"
	"testing"
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

