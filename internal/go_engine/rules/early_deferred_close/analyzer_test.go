package early_deferred_close

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa5001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA5001, copied unchanged from staticcheck/sa5001/testdata/go1.0/CheckEarlyDefer into
// testdata/src/CheckEarlyDefer, holds the port to every `//@ diag(...)` note the original expects, and to nothing
// more. The go1.0 folder sets the language version of staticcheck's run, which SA5001 never reads, so the fixture
// loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckEarlyDefer")
}

// Over staticcheck's fixture and parity's, the second holding every shape of assignment, result type and deferred
// call the check reads, the original and the port find the same findings, the message printing the call and the
// deferred call.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5001.Analyzer, Analyzer, analysistest.TestData(), "CheckEarlyDefer", "parity")
}

// The differences fixture holds the calls whose printing the two could most plausibly part on, a composite literal's
// method, explicit type arguments, a comment inside the deferred call and a literal called at once. It records no
// difference, since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5001.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
