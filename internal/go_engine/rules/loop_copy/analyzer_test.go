package loop_copy

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreams are staticcheck's own fixtures for S1001, each copied unchanged, golden file and all, from
// simple/s1001/testdata/go1.0/CheckLoopCopy and go1.18/CheckLoopCopy into testdata/src under the same two folders,
// since the two fixtures share one folder name. The go1.N folder sets the language version of staticcheck's run,
// which S1001 never reads, gating nothing, so the fixtures load from a tree laid out by import path as the house's
// fixtures do, where the generic one type-checks under the newest language.
var upstreams = []string{"go1.0/CheckLoopCopy", "go1.18/CheckLoopCopy"}

// fixtures is every fixture package the tree holds where the two agree outside the differences fixture,
// staticcheck's and the port's own: parity's and its package that declares len and copy at package scope.
var fixtures = []string{"go1.0/CheckLoopCopy", "go1.18/CheckLoopCopy", "parity", "parity/shadowed"}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreams...)
}

// The port's fixes, applied to each upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. Each finding suggests one fix, and neither golden file holds sections.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstreams...)
}

// The original and the port run in one checker over staticcheck's fixtures and parity's, the second holding every
// shape the query and the judgment branch on and the near miss of each, a generated file, and a package that declares
// len and copy at package scope, and their findings must be the same multiset, each the same to its end and its
// fixes. The fixes are built from what the query binds, through edit.ReplaceWithNode on both sides, so this is where
// the bindings are held to the original's, their parentheses removed, and the printed loops replaced byte for byte.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1001.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// The differences fixture holds the shapes where the two could most plausibly part, identifiers with no object in a
// function literal the destination holds, dereferences the fixes print, sources the same only by syntax and calls
// ranged over twice, and a key spelled like an outer variable, and records none, since the port parts from the
// original nowhere. The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing, so the two are
// required to agree there finding for finding, fixes and all, in a call of its own, which fails when the original
// finds nothing there.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1001.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The pattern looks through parentheses around a ranged source and doubled ones, and the parser leaves an empty
// statement after a store followed by two semicolons, shapes the house's testdata cannot hold, since its guard holds
// testdata to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a
// tree of the test's own. The two are held the same there to each finding's end and its fixes, the
// gofmt-rewritable source's printed loops included.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1001.Analyzer, Analyzer, unformattedTree(test), "unformatted")
}

// unformattedTree writes the unformatted fixture out as Go into a tree laid out by import path in a directory of the
// test's own, and returns the tree. No call of the parity harness writes a fixture out before it loads it, so this
// stays the port's own.
func unformattedTree(test *testing.T) string {
	test.Helper()
	source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "unformatted", "unformatted.go.txt"))
	if readError != nil {
		test.Fatal(readError)
	}
	directory := test.TempDir()
	packageDirectory := filepath.Join(directory, "src", "unformatted")
	if makeError := os.MkdirAll(packageDirectory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(filepath.Join(packageDirectory, "unformatted.go"), source, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return directory
}
