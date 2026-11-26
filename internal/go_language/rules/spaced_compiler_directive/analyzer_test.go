package spaced_compiler_directive

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/staticcheck/sa9009"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
	"github.com/system-inc/cohere/internal/go_language/check/parity/staticcheck_fixtures"
)

// upstreamPackage is the import path upstreamTree gives the fixture, its folder's name in staticcheck's testdata.
const upstreamPackage = "Directives"

// upstreamTree is a tree laid out by import path, made for the test, holding staticcheck's fixture for SA9009 byte for
// byte as the release keeps it, which the harness loads as it loads testdata. The go1.0 folder sets the language
// version of staticcheck's run, which SA9009 never reads.
//
// The check judges whether a comment starts in its line's first column, and the fixture's cases are comments indented
// before it, which gofmt would move there, so a gofmt-clean copy would test nothing; the repository's gofmt guard walks
// testdata, so staticcheck_fixtures keeps it under another name, and this copies it into a tree of the test's own.
func upstreamTree(test *testing.T) string {
	test.Helper()
	upstreamFixture := filepath.Join(staticcheck_fixtures.Directory(test, "staticcheck", "sa9009", "testdata", "go1.0",
		"Directives"), "pkg.go")
	contents, readError := os.ReadFile(upstreamFixture)
	if readError != nil {
		test.Fatalf("reading staticcheck's fixture for SA9009, which staticcheck_fixtures keeps: %v", readError)
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
// Each note sits in the comment it marks, which the message quotes whole, so the note's pattern always matches its own
// text there: this holds where the port reports, and TestPortReportsUpstreamMessagesWordForWord holds what it says.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, upstreamTree(test), upstreamPackage)
}

// upstreamMessages is every finding the port makes over staticcheck's fixture, by line, with its message written out
// here rather than read from the fixture, whose notes the messages quote. The second comment's tab after its slashes
// reads `\t` in the message, as %q writes it.
var upstreamMessages = []struct {
	line    int
	message string
}{
	{5, "ineffectual compiler directive due to extraneous space: " +
		"\"// go:linkname bad1 foo.Bad1 //@ diag(`ineffectual compiler directive`)\""},
	{9, "ineffectual compiler directive due to extraneous space: " +
		"\"//\\tgo:foobar sdlkfjlaksdj //@ diag(`ineffectual compiler directive`)\""},
}

// Over staticcheck's fixture the port says word for word what upstreamMessages holds, so a message that lost a word
// or its quoting fails here, where the fixture's notes, matched against a message that quotes them, cannot tell.
func TestPortReportsUpstreamMessagesWordForWord(test *testing.T) {
	test.Parallel()
	_, portFindings := parity.Findings(test, sa9009.Analyzer, Analyzer, upstreamTree(test), upstreamPackage)
	if len(portFindings) != len(upstreamMessages) {
		test.Fatalf("the port made %d findings over staticcheck's fixture, and %d are written out: %v",
			len(portFindings), len(upstreamMessages), portFindings)
	}
	for index, expected := range upstreamMessages {
		if portFindings[index].Line != expected.line || portFindings[index].Message != expected.message {
			test.Errorf("finding %d is %q on line %d, and line %d says %q", index, portFindings[index].Message,
				portFindings[index].Line, expected.line, expected.message)
		}
	}
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
