package boolean_return_of_condition

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/simple/s1008"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for S1008, copied unchanged from simple/s1008/testdata/go1.0/CheckIfReturn
// into testdata/src/CheckIfReturn. The go1.0 folder sets the language version of staticcheck's run, which S1008 never
// reads, so the fixture loads from a tree laid out by import path as the house's fixtures do. It holds no golden
// file, since S1008 suggests no fix.
const upstream = "CheckIfReturn"

// fixtures is every fixture package both sides run over beside the upstream one: parity's, holding every shape the
// check branches on and the near miss of each and a generated file, and two packages that declare their own true and
// len at package scope, which a package of their own holds.
var fixtures = []string{"parity", "shadowed_boolean", "shadowed_length"}

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The original and the port run in one checker over staticcheck's fixture and the parity fixtures, and their findings
// must be the same multiset, each the same to its end, the if statement as its block holds it, label and block
// included, and its fixes. S1008 suggests no fix, so a fix either side made would be a difference.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1008.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat([]string{upstream}, fixtures)...)
}

// The shapes gofmt rewrites, a parenthesized if condition, which it strips, doubled parentheses, which it folds, and
// a pair on one line, are held the same way, to each finding's end. The guard holds every .go file of the tree to
// gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the
// test's own. The writing out stays the port's own, since no harness call writes a fixture out before it loads.
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
	parity.RequireIdentical(test, s1008.Analyzer, Analyzer, directory, "unformatted")
}

// The differences fixture holds the shapes where the two could most plausibly part, labels and blocks around either
// statement of the pair and inside the body, an empty else, a labeled if and a block before the pair, type
// parameters' conditions and lengths, and function literals in and around the pair, and records none, since the port
// parts from the original nowhere. The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing,
// so the two are required to agree there finding for finding, to each finding's end, in a call of its own, which
// fails when the original finds nothing there.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1008.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
