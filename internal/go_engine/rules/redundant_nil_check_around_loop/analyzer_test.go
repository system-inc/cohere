package redundant_nil_check_around_loop

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1031"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreams are staticcheck's own fixtures for S1031, each copied unchanged from simple/s1031/testdata/go1.0/
// CheckNilCheckAroundRange and go1.18/CheckNilCheckAroundRange into testdata/src under the same two folders, since
// the two fixtures share one folder name. The go1.N folder sets the language version of staticcheck's run, which
// S1031 never reads, gating nothing, so the fixtures load from a tree laid out by import path as the house's fixtures
// do, where the generic one type-checks under the newest language. Neither holds a golden file, since S1031 suggests
// no fix.
var upstreams = []string{"go1.0/CheckNilCheckAroundRange", "go1.18/CheckNilCheckAroundRange"}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreams...)
}

// Over staticcheck's fixtures and parity's, the second holding every shape the query and the judgment branch on and
// the near miss of each, a generated file, and a package that declares nil at package scope, the original and the
// port find the same multiset, each finding the same to its end and its fixes. S1031 suggests no fix, so the ends are
// what this holds beyond the start and message, the if keyword through its condition, and a fix either side made
// would be a difference.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1031.Analyzer, Analyzer, analysistest.TestData(), "go1.0/CheckNilCheckAroundRange",
		"go1.18/CheckNilCheckAroundRange", "parity", "parity/shadowed")
}

// The differences fixture holds the shapes where the two could most plausibly part, labels and blocks around the
// range at more than one depth, an alias of a named slice, a constraint with a method beside its slice term, else if
// chains, a closure's shadowed variable and nested findings, and records none, since the port parts from the original
// nowhere. RequireOnlyAdjudicated fails a fixture that records nothing, so the two are required to agree there as
// everywhere else, in a call of its own, which fails when the original finds nothing there, so the agreement is over
// findings.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1031.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The pattern looks through parentheses around the condition and the ranged value, doubled ones too, and the parser
// keeps an empty statement before a range or in an else, shapes the house's testdata cannot hold, since its guard
// holds testdata to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go
// into a tree of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1031.Analyzer, Analyzer, unformattedTree(test), "unformatted")
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
