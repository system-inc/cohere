package infinite_loop_condition

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1006"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1006, copied unchanged from simple/s1006/testdata/go1.0/CheckForTrue into
// testdata/src/CheckForTrue. The go1.0 folder sets the language version of staticcheck's run, which S1006 never
// reads, so the fixture loads from a tree laid out by import path as the house's fixtures do. It holds no golden file,
// since S1006 suggests no fix.
const upstream = "CheckForTrue"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects. One note sits in input.go: a loop of generated.go after a
// line directive into input.go is displayed there, asked about by that name, found not generated, and reported.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	fixture := filepath.Join(analysistest.TestData(), "src", upstream)
	wants, readError := staticcheck_wants.Read(fixture)
	if readError != nil {
		test.Fatal(readError)
	}
	if len(wants) == 0 {
		test.Fatal("the upstream fixture holds no notes, so the comparison proves nothing")
	}
	_, port := parity.Findings(test, s1006.Analyzer, Analyzer, analysistest.TestData(), upstream)
	found := []staticcheck_wants.Finding{}
	for _, each := range port {
		// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
		found = append(found, staticcheck_wants.Finding{
			File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
		})
	}
	test.Logf("the fixture expects %d, the port found %d", len(wants), len(found))
	unexpected, untaken := staticcheck_wants.Compare(wants, found)
	for _, extra := range unexpected {
		test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
			extra.Message)
	}
	for _, missing := range untaken {
		test.Errorf("the port reported nothing matching %s", missing)
	}
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every
// shape the check branches on and the near miss of each, and a generated file, and their findings must be the same
// multiset, each the same to its end, the loop's header from its keyword to the end of its condition, and its fixes.
// S1006 suggests no fix, so a fix either side made would be a difference.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1006.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The shapes gofmt rewrites, a parenthesized condition and a loop of empty clauses, are held the same way, to each
// finding's end. The guard holds every .go file of the tree to gofmt, so the fixture is kept as
// testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	directory := unformattedTree(test)
	parity.RequireIdentical(test, s1006.Analyzer, Analyzer, directory, "unformatted")
}

// The differences fixtures hold the shapes where the two could most plausibly part, a constant reached through a dot
// import, constants made by iota and conversion, a parameter of a type constrained by ~bool, and in a package of its
// own true and false shadowed by constants of each other's value, and record none, since the port parts from the
// original nowhere. The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing, so the two are
// required to agree there finding for finding, to each finding's end, in a call of its own, which fails when the
// original finds nothing there.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1006.Analyzer, Analyzer, analysistest.TestData(), "differences", "shadowed")
}

// unformattedTree writes testdata/unformatted/unformatted.go.txt out as Go into a tree of the test's own, laid out by
// import path with the package at unformatted, and returns the tree's directory.
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
