package single_case_select

import (
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1000"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1000, copied unchanged from simple/s1000/testdata/go1.0/
// CheckSingleCaseSelect into testdata/src/CheckSingleCaseSelect. The go1.0 folder sets the language version of
// staticcheck's run, which S1000 never reads, so the fixture loads from a tree laid out by import path as the house's
// fixtures do. It holds no golden file, since S1000 suggests no fix.
const upstream = "CheckSingleCaseSelect"

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
	_, port := parity.Findings(test, s1000.Analyzer, Analyzer, analysistest.TestData(), upstream)
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

// Over staticcheck's fixture, parity's, which holds every shape the two queries branch on and the near miss of each,
// and a generated file, and the differences fixture's, the original and the port find the same multiset, each finding
// the same to its end. S1000 suggests no fix, so the ends are what this holds beyond the starts and messages, the
// whole for statement and the select keyword, and a fix either side made would be a difference.
//
// The differences fixture holds the shapes where the two could most plausibly part, a type parameter's channel, labels
// and blocks around the select at more than one depth, a labeled for as the body's sole statement, assignments to
// every kind of left side, selects nested in a case and in a function literal, and records none, since the port parts
// from the original nowhere. RequireOnlyAdjudicated fails a fixture that records nothing, so it is held here, finding
// for finding, with the rest.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1000.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity", "differences")
}
