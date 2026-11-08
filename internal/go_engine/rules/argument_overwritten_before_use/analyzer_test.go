package argument_overwritten_before_use

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4009"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA4009, copied unchanged from its testdata/go1.0 into testdata/src/CheckArgOverwritten,
// holds the port to every `//@ diag` note the original expects, and to nothing more. The fixture uses nothing newer
// than Go 1.0, so it needs no version of its own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckArgOverwritten")
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings. Staticcheck's report gives a finding the end of its parameter's name as well as its
// start, and the assignment that overwrites it as a related entry, so the end and the related entry are held too.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4009.Analyzer, Analyzer, analysistest.TestData(), "CheckArgOverwritten", "parity")
}

// The differences fixture holds the cases where the two could plausibly disagree. It records no difference, since the
// two read the same syntax and the same form and differ nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4009.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
