package go_engine

import (
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/needs"
)

// buildCensusVariable names the tree the build census runs the engine over: root for cohere's own module, or a
// directory. The run is cold and writes the tree's store, so the tree's `.cache/cohere/go` is to be empty, or moved
// aside, first.
const buildCensusVariable = "COHERE_GO_IR_BUILD_CENSUS"

// The build census (#wt5vaft): a cold run of the engine over the tree COHERE_GO_IR_BUILD_CENSUS names, which writes
// every entry with its answers computed whole through the resolver, builds each package's source functions in one
// program, a root's in the one its readers and the resolver share. It logs how many packages were built in how many
// programs and fails naming any built in more than one. Unset, it skips, since a tree takes minutes. It runs alone,
// never in parallel, so the census counts this run's builds and no other test's.
//
// Not parallel: an open census counts every build in the process, so a test beside it would add its own.
func TestEachPackageIsBuiltOnceOverATree(test *testing.T) {
	directory := needs.OptIn(test, buildCensusVariable, "a census of the builds of a cold run over root or a directory")
	if directory == "root" {
		// This package sits two directories below the module root.
		root, rootError := filepath.Abs(filepath.Join("..", ".."))
		if rootError != nil {
			test.Fatal(rootError)
		}
		directory = root
	}
	census := intermediate_representation.OpenBuildCensus()
	ran := runCachedEngine(test, directory)
	census.Close()
	if ran.counts.Hit > 0 || ran.counts.Written == 0 {
		test.Fatalf("want a cold run that writes entries, got %+v", ran.counts)
	}
	byPrograms := map[int]int{}
	var twice []string
	for packageTypes, programs := range census.Packages() {
		byPrograms[programs]++
		if programs > 1 {
			twice = append(twice, packageTypes.Path())
		}
	}
	for _, programs := range slices.Sorted(maps.Keys(byPrograms)) {
		test.Logf("%s: %d packages built in %d program(s)", directory, byPrograms[programs], programs)
	}
	test.Logf("%s: the run wrote %d entries", directory, ran.counts.Written)
	if len(twice) > 0 {
		slices.Sort(twice)
		test.Fatalf("%d packages were built in more than one program: %v", len(twice), twice)
	}
}
