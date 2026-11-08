package invalid_url

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1007"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA1007, copied unchanged from staticcheck/sa1007/testdata/go1.0/CheckURLs
// into testdata/src/CheckURLs. The go1.0 folder sets the language version of staticcheck's run, which no part of
// SA1007 reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckURLs"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the second holding the shapes staticcheck's representation builds an
// argument or a call in more than one way, the original and the port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1007.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck, and records no
// difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1007.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
