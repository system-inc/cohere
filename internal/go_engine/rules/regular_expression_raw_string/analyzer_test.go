package regular_expression_raw_string

import (
	"testing"

	"honnef.co/go/tools/simple/s1007"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
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
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
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
