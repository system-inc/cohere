package removed_user_directory

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa9007"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackage is staticcheck's own fixture for SA9007, copied unchanged from its testdata/go1.0 into
// testdata/src. The fixture uses nothing newer than Go 1.0, so it needs no version of its own.
const upstreamPackage = "CheckBadRemoveAll"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackage)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, related entries
// included. parity holds the shapes the check depends on.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9007.Analyzer, Analyzer, analysistest.TestData(), upstreamPackage, "parity")
}

// The differences fixture holds the cases where the two could plausibly disagree: where the related entry points, at
// an extract or at the call it reads, and where phis form a cycle that flattening must cut. It records no difference,
// since the two read the same form and differ nowhere, so they must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9007.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
