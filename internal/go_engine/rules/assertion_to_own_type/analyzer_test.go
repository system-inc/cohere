package assertion_to_own_type

import (
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1040"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1040, copied unchanged from
// simple/s1040/testdata/go1.0/CheckSameTypeTypeAssertion into testdata/src/CheckSameTypeTypeAssertion. It has no
// golden file, since S1040 suggests no fix. The go1.0 folder sets the language version of staticcheck's run, which
// S1040 never reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckSameTypeTypeAssertion"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
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
	_, port := parity.Findings(test, s1040.Analyzer, Analyzer, analysistest.TestData(), upstream)
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

// Over staticcheck's fixture and parity's, the second holding every shape the judgment branches on and the near miss
// of each, values and types written with parentheses, over several lines and with comments, and a generated file, the
// original and the port find the same multiset, each finding the same to its end and its fixes. S1040 suggests no
// fix, so this holds the ends beyond the start and message, which span the whole assertion.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1040.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the two could most plausibly part, an interface shadowed in an inner
// scope, embedded interfaces against the literal of their methods, nested generic instantiations, an assertion in a
// closure and as an argument, and an alias of the universe's error, and records none, since the port parts from the
// original nowhere. RequireOnlyAdjudicated fails a fixture that records nothing, so the two are required to agree
// there as everywhere else, in a call of its own, which fails when the original finds nothing there, so the agreement
// is over findings.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1040.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
