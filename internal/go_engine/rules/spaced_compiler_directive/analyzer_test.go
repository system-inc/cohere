package spaced_compiler_directive

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/staticcheck/sa9009"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamFixture is staticcheck's own fixture for SA9009, read where the vendored module keeps it. The check judges
// whether a comment starts in its line's first column, and the fixture's cases are comments indented before it, which
// gofmt would move there, so a gofmt-clean copy would test nothing; the repository's gofmt guard walks testdata, so an
// unchanged copy cannot live there. upstreamTree copies it byte for byte into a tree of the test's own instead.
var upstreamFixture = filepath.Join("..", "..", "..", "..", "third_party", "honnef.co", "go", "tools", "staticcheck",
	"sa9009", "testdata", "go1.0", "Directives", "pkg.go")

// upstreamPackage is the import path upstreamTree gives the fixture, its folder's name in staticcheck's testdata.
const upstreamPackage = "Directives"

// upstreamTree is a tree laid out by import path, made for the test, holding staticcheck's fixture for SA9009 byte for
// byte as the vendored module keeps it, which the harness loads as it loads testdata. The go1.0 folder sets the
// language version of staticcheck's run, which SA9009 never reads.
func upstreamTree(test *testing.T) string {
	test.Helper()
	contents, readError := os.ReadFile(upstreamFixture)
	if readError != nil {
		test.Fatalf("reading staticcheck's fixture for SA9009, which the vendored module must keep: %v", readError)
	}
	tree := test.TempDir()
	directory := filepath.Join(tree, "src", upstreamPackage)
	if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	copied := filepath.Join(directory, filepath.Base(upstreamFixture))
	if writeError := os.WriteFile(copied, contents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return tree
}

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, upstreamTree(test), upstreamPackage)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds every way a
// comment can read as a directive or not that gofmt leaves standing, and a generated file. The two sit in two trees,
// staticcheck's in the test's own, so each is compared in a call of its own.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9009.Analyzer, Analyzer, upstreamTree(test), upstreamPackage)
	parity.RequireIdentical(test, sa9009.Analyzer, Analyzer, analysistest.TestData(), "parity")
}

// The differences fixture holds the comments where the two could most plausibly part: a line directive moving the
// columns after it, a directive in a comment group's middle, and quotes and backslashes the message quotes. It
// records no difference, since the port parts from the original nowhere, so the two must agree there finding for
// finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9009.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
