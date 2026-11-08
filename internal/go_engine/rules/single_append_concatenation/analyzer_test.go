package single_append_concatenation

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1011"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for S1011, copied unchanged from simple/s1011/testdata/go1.0/CheckLoopAppend
// into testdata/src/CheckLoopAppend with its golden file. The go1.0 folder sets the language version of staticcheck's
// run, which S1011 never reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckLoopAppend"

// fixtures is every fixture package where the original and the port agree, fixes and all: staticcheck's; parity, which
// holds every shape the query's three alternatives and the checks after it branch on, the near miss of each, and a
// generated file, which both filter; shadow, which declares an append of its own at package scope; and walk, which
// holds every place the port's walk over the functions where syntax starts must reach, each once.
var fixtures = []string{upstream, "parity", "shadow", "walk"}

// differencesFixture is the fixture where the port parts from the original, each part recorded on its line, and the
// shapes where the two could most plausibly part and agree.
const differencesFixture = "differences"

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The harness keys each side by the analyzer itself, so it would hold
// a port under another name as readily as under S1011's; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1011.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1011.Analyzer.Name)
	}
}

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. The golden file holds no sections.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and the parity fixtures, the original and the port find the same multiset, each finding
// the same to its end, its related entries and its fixes, every edit's offsets and text.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1011.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// A slice ranged over in parentheses, which the pattern language looks through and its binding stores without, so
// both messages print it bare, is a shape gofmt rewrites. The guard holds every .go file of the tree to gofmt, so the
// fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own,
// which the harness then loads as it loads testdata. No call of the parity harness writes a fixture out before it
// loads it, so the writing stays this test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
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
	parity.RequireIdentical(test, s1011.Analyzer, Analyzer, directory, "unformatted")
}

// Over the differences fixture, the port's findings are the original's but where a record says otherwise: the port
// drops a loop whose append spreads its element, in each of the three alternatives, and withholds the fix of a loop
// whose key or value is a variable declared outside the for statement, read after the loop or not, a parameter or
// another package's through a dot import. Every shape no record is on, a loop variable shadowed in the body, a slice
// that is ranged over and appended to, an element held in a variable spelled like its slice, a key the header defines
// and a loop in a function literal, must be the original's to the byte.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1011.Analyzer, Analyzer, analysistest.TestData(), differencesFixture)
}
