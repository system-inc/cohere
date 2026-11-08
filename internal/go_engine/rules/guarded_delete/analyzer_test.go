package guarded_delete

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1033"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for S1033, copied unchanged from
// simple/s1033/testdata/go1.0/CheckGuardedDelete into testdata/src/CheckGuardedDelete with its golden file. The go1.0
// folder sets the language version of staticcheck's run, which S1033 never reads, so the fixture loads from a tree
// laid out by import path as the house's fixtures do.
const upstream = "CheckGuardedDelete"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. The golden file holds no sections, so its comment is the whole fixed file.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the second holding every shape the query branches on and the near miss of
// each, parentheses and blocks looked through, maps and keys compared as syntax, a generated file, and delete
// shadowed in a block and at package scope, the original and the port find the same multiset, each finding the same
// to its end and its fixes. Both print the bound call through edit.ReplaceWithNode, so this is where the call the
// port finds is held to be the very node the pattern binds, byte for byte, a call in parentheses, in nested blocks,
// across lines and with comments around and inside it included.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1033.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity",
		"parity/shadowed")
}

// The shapes gofmt rewrites, parentheses it folds or drops, the condition's among them, a body on the if's line, and
// an empty statement beside the call, which makes the body two statements, are held the same way. The guard holds
// every .go file of the tree to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written
// out as Go into a tree of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1033.Analyzer, Analyzer, unformattedTree(test), "unformatted")
}

// The differences fixture holds the shapes where the two could most plausibly part, parentheses deep in the map and
// the key, function literals whose blocks match.go compares by its own order, and a fix whose call spans lines among
// comments, and records none, since the port parts from the original nowhere. RequireOnlyAdjudicated fails a fixture
// that records nothing, so the two are required to agree there finding for finding, fixes and all, in a call of its
// own, which fails when the original finds nothing there.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1033.Analyzer, Analyzer, analysistest.TestData(), "differences")
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
