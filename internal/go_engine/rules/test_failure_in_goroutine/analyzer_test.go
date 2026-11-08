package test_failure_in_goroutine

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa2002"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA2002, copied unchanged from its testdata/go1.0 into testdata/src. The
// fixture uses nothing newer than Go 1.0, so it needs no version of its own.
const upstream = "CheckConcurrentTesting"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings, each with its related entry at the call that stops the test.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa2002.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the cases where the two could plausibly disagree, a function started from many
// places, from itself, or from one judged before or after it, and a go statement reporting many calls, each with a
// related entry. It records no difference, since the two read the same form and differ nowhere, so they must agree
// there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa2002.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
