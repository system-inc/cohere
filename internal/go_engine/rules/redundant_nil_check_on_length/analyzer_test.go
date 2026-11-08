package redundant_nil_check_on_length

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/simple/s1009"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackages is staticcheck's own fixtures for S1009, copied unchanged from simple/s1009/testdata/go1.0 and
// testdata/go1.18, each into testdata/src under its version folder, since both are the folder
// CheckRedundantNilCheckWithLen. The version folder sets the language version of staticcheck's run, which S1009 never
// reads, so the fixtures load from a tree laid out by import path, with the toolchain's version, which the generic
// one needs and the other does not mind. They hold no golden file, since S1009 suggests no fix.
var upstreamPackages = []string{"go1.0/CheckRedundantNilCheckWithLen", "go1.18/CheckRedundantNilCheckWithLen"}

// parityPackages is the fixtures written for the comparison: parity, which holds every shape the query and the
// original's judgments branch on and the near miss of each, and a generated file, which both filter; and shadow,
// which declares a len of its own at package scope.
var parityPackages = []string{"parity", "shadow"}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under any other name as readily as under S1009's; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1009.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1009.Analyzer.Name)
	}
}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// Over staticcheck's fixtures and the parity fixtures, the original and the port find the same multiset, each finding
// the same to its end, the whole outer expression, and its fixes. S1009 suggests no fix, so a fix either side made
// would be a difference.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1009.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, parityPackages)...)
}

// The pattern looks through doubled parentheses, a shape the house's testdata cannot hold, since its guard holds
// testdata to gofmt, which folds them to one pair, so the fixture is kept as testdata/unformatted/unformatted.go.txt
// and written out as Go into a tree of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1009.Analyzer, Analyzer, unformattedTree(test), "unformatted")
}

// The differences fixture holds the shapes where the two could most plausibly part, a bound that is negative or zero
// but no integer, against every comparison the original takes. The port parts from the original there, where the
// original is wrong, each finding recorded on its line: `// port drops: <the original's message>` for a nil check
// that changes the answer, which the original reports, and `// port finds: <message>` for a redundant one it leaves
// alone. Every other finding there the two make the same, to its end.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1009.Analyzer, Analyzer, analysistest.TestData(), "differences")
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
