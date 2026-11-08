package timer_reset_result_used

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1025"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackage is staticcheck's own fixture for SA1025, copied unchanged from its testdata/go1.0 into
// testdata/src. The fixture uses nothing newer than Go 1.0, so it needs no version of its own.
const upstreamPackage = "CheckTimerResetReturnValue"

// Staticcheck's own fixture marks each expected finding with `//@ diag(...)`, and holds the port to every mark the
// original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackage)
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1025.Analyzer, Analyzer, analysistest.TestData(), upstreamPackage, "parity")
}

// The differences fixture holds the cases where the two could plausibly disagree, and records nothing, because the
// two read the same form and differ nowhere, so they find the same findings there.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1025.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
