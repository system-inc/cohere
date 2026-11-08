package sliding_copy

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1018"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreams are staticcheck's own fixtures for S1018, each copied unchanged, golden file and all, from
// simple/s1018/testdata/go1.0/CheckLoopSlide and go1.18/CheckLoopSlide into testdata/src under the same two folders,
// since the two fixtures share one folder name. The go1.N folder sets the language version of staticcheck's run,
// which S1018 never reads, gating nothing, so the fixtures load from a tree laid out by import path as the house's
// fixtures do, where the generic one type-checks under the newest language.
var upstreams = []string{"go1.0/CheckLoopSlide", "go1.18/CheckLoopSlide"}

// fixtures is every fixture package the tree holds where the original and the port agree, staticcheck's and parity's.
var fixtures = []string{"go1.0/CheckLoopSlide", "go1.18/CheckLoopSlide", "parity"}

// differencesFixtures is the differences fixture's packages: its own, one that declares copy at package scope, and
// one that imports a package under the name copy, the last two scopes where copy names no builtin.
var differencesFixtures = []string{"differences", "differences/shadowed", "differences/imported"}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreams...)
}

// The port's fixes, applied to each upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. The fixed file is gofmt'd before it is compared, which is what drops the blank line the replaced loop
// leaves. Each finding suggests one fix, and neither golden file holds sections.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstreams...)
}

// The original and the port run in one checker over staticcheck's fixtures and parity's, the second holding every
// shape the query and the judgment branch on and the near miss of each, and a generated file, and their findings must
// be the same multiset, each the same to its end and its fixes. The original builds its fix through code.EditMatch
// from the identifiers its query binds, and the port through edit.ReplaceWithNode from the ones it binds, so this is
// where the bindings are held to the original's, their parentheses removed, and the loops replaced byte for byte.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1018.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// In each package of the differences fixture, the port's findings are the original's but where a record says the
// port drops a finding, a counter left where it stood or one that is its own offset or limit, or withholds its fix,
// where copy names no builtin at the loop or the counter outlives it. Every shape no record is on, the counter named
// copy, a copy declared after the loop, identifiers across lines and nested loops, is the original's to the byte.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	for _, fixture := range differencesFixtures {
		test.Run(fixture, func(test *testing.T) {
			test.Parallel()
			parity.RequireOnlyAdjudicated(test, s1018.Analyzer, Analyzer, analysistest.TestData(), fixture)
		})
	}
}

// The pattern looks through parentheses around the loop's condition and doubled ones, and the parser leaves an empty
// statement after a store followed by two semicolons, shapes the house's testdata cannot hold, since its guard holds
// testdata to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a
// tree of the test's own, where the two are held the same to the end and fixes of every finding.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1018.Analyzer, Analyzer, unformattedTree(test), "unformatted")
}

// unformattedTree writes the unformatted fixture out as Go into a tree laid out by import path in a directory of the
// test's own, and returns the tree. It stays the port's own, since no harness call writes a fixture kept as text out
// as Go; the comparison over the tree is the harness's.
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
