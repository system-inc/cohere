package nanosecond_sleep

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1004"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA1004, copied unchanged from
// staticcheck/sa1004/testdata/go1.0/CheckTimeSleepConstant into testdata/src/CheckTimeSleepConstant with its golden
// file. The go1.0 folder sets the language version of staticcheck's run, which SA1004 never reads, so the fixture
// loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckTimeSleepConstant"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to staticcheck's fixture, make its golden file, as staticcheck's testutil requires of the
// original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, fixes included. parity
// holds every way the query accepts or refuses a call: the callee through parentheses, a dot and a renamed import, a
// method value and functions named Sleep that are not time's, the argument as an integer literal in every base and
// under unary signs and parentheses, at and around the bounds, and as named constants, conversions and arithmetic, in
// a generated file too.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1004.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity",
		"parity/dotted")
}

// The differences fixture holds the shapes where the fix's printing could part, a literal spread over lines, beside
// comments and inside parentheses. It records no difference, since there is none, so the two must agree there finding
// for finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1004.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
