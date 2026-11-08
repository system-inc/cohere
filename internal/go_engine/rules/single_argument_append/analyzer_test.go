package single_argument_append

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4021"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA4021, copied unchanged from its testdata/go1.0 into
// testdata/src/CheckSingleArgAppend, holds the port to every `//@ diag(...)` note the original expects, and to nothing
// more. The fixture uses nothing newer than Go 1.0, so it needs no version of its own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckSingleArgAppend")
}

// Over staticcheck's fixture, parity's, holding the shapes the check depends on, and shadowed's, which declares append
// at package scope, the original and the port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4021.Analyzer, Analyzer, analysistest.TestData(), "CheckSingleArgAppend", "parity",
		"shadowed")
}

// The differences fixture holds the cases where the two could plausibly disagree. It records nothing, because the two
// read the same form and differ nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4021.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
