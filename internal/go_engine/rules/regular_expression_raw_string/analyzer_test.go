package regular_expression_raw_string

import (
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1007"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1007, copied unchanged from simple/s1007/testdata/go1.0/CheckRegexpRaw
// into testdata/src/CheckRegexpRaw. It has no golden file, since S1007 suggests no fix. The go1.0 folder sets the
// language version of staticcheck's run, which S1007 never reads, so the fixture loads from a tree laid out by import
// path as the house's fixtures do.
const upstream = "CheckRegexpRaw"

// fixtures is every fixture package where the two agree outside the differences fixture, staticcheck's and the
// port's own: parity's and its dot import.
var fixtures = []string{upstream, "parity", "parity/dotted"}

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
	_, port := parity.Findings(test, s1007.Analyzer, Analyzer, analysistest.TestData(), upstream)
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
// shape the check branches on and the near miss of each, a generated file, a dot and a renamed import, and their
// findings must be the same multiset, each the same to its end, a parenthesized literal's above all, and its fixes.
// S1007 suggests no fix, so a fix either side made would be a difference.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1007.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// The differences fixture holds the shapes where the two could most plausibly part, calls in a package variable's
// composite literal, in a generic function and in a field's value, a local that takes the name regexp, and escaped
// quotes beside escaped backslashes, and records none, since the port parts from the original nowhere. The parity
// harness's RequireOnlyAdjudicated fails a fixture that records nothing, so the two are required to agree there
// finding for finding, to each finding's end, in a call of its own, which fails when the original finds nothing
// there.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1007.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
