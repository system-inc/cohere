package main

import (
	"os"
	"path/filepath"
)

// raceGated are the packages whose tests the land gate runs a second time under -race (#bnabymg). A concurrency test
// catches its race only under the race detector, and the gate's go test runs without it, so a change that put back
// the race the test was written for landed green: TestDependenceSetsComputedAtOnceOverOneHostAgree was red with 26
// races with its lock removed, but only by hand.
//
// The run is Home's, after the module's plain run passes there (home_verdict.go): measured on the Mac on 2026-11-12,
// the five heaviest of these took 591s of wall and about 1,050 CPU-seconds under -race one after another with their
// race build cached, against a gate of about seven minutes, so not on every landing on the Mac. A landing Home gives
// no verdict for runs no -race, and its land line says so.
//
// Every package holding a test whose name or file says it runs code at once is here, which
// TestEveryConcurrencyTestIsRaceGated holds, and a package may be here for a reason its test names don't show.
var raceGated = []raceEntry{
	{"command/cohere", "TestFormatInParallelLeavesTheRulesToTheSerialPath formats files at once " +
		"(fix_parallel_test.go)"},
	{"internal/edit", "TestConcurrentReadersNeverSeeAPartialFile reads while a write replaces the file"},
	{"internal/executable_testing", "TestAScriptWrittenWhileOthersForkRunsAtOnce writes and runs scripts while " +
		"other goroutines fork (#g93ts58)"},
	{"internal/go_engine/bounded", "its worker pool runs a function on every index at once, as many as there " +
		"are cores"},
	{"internal/go_engine/cache", "TestConcurrentPutsOfOneKeyLeaveAWholeEntry writes one key from several goroutines"},
	{"internal/go_engine/check", "dependence sets and type graph digests are computed at once over one host " +
		"(dependence_concurrency_test.go, type_graph_concurrency_test.go)"},
	{"internal/go_engine/check/checker", "the scheduler runs groups and actions at once, up to its slots and workers"},
	{"internal/go_engine/intermediate_representation", "the intermediate representation is built in parallel " +
		"(parallel_build_test.go)"},
	{"internal/go_engine/load", "the loader lists and type-checks packages side by side"},
	{"internal/go_engine/suppression", "TestBuildConcurrently builds suppression tables from several goroutines"},
	{"internal/release/dispatch", "TestInstallingTheEngineConcurrentlyLeavesOneBinary installs from two runs at once"},
	{"internal/types/program", "programs check, parse libraries and record their run cache at once " +
		"(checker_concurrency_test.go, library_parses_test.go, run_cache_test.go)"},
	{"internal/write_root", "TestCopiesAskedAtOnceAgree asks for one copy from several goroutines"},
}

// raceEntry is one package the gate runs under -race, by its directory under the module root, and why.
type raceEntry struct {
	directory string
	reason    string
}

// racePatterns is the go test patterns of the raceGated packages that exist in the tree at root, so a tree that
// holds none of them, a fixture's, runs no -race at all.
func racePatterns(root string) []string {
	patterns := []string{}
	for _, entry := range raceGated {
		if information, statError := os.Stat(filepath.Join(root, entry.directory)); statError == nil &&
			information.IsDir() {
			patterns = append(patterns, "./"+entry.directory)
		}
	}
	return patterns
}
