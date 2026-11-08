package never_nil_comparison

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4031"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// Staticcheck's own fixture for SA4031, copied unchanged from its testdata/go1.0 into testdata/src, holds the port to
// every note the original expects, and to nothing more. The fixture uses nothing newer than Go 1.0, so it needs no
// version of its own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckAllocationNilCheck")
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings, related places and all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4031.Analyzer, Analyzer, analysistest.TestData(), "CheckAllocationNilCheck",
		"parity")
}

// The differences fixture records nothing, because the two read the same form and differ nowhere: it holds the cases
// where they could plausibly disagree, so the test holds that they agree there.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4031.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
