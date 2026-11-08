package cyclic_finalizer

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa5005"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA5005, copied unchanged from its testdata/go1.0 into
// testdata/src/CheckCyclicFinalizer, holds the port to every `//@ diag(...)` note the original expects, and to nothing
// more. The fixture uses nothing newer than Go 1.0, so it needs no version of its own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckCyclicFinalizer")
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings, the end of the call's syntax held as well as its start.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5005.Analyzer, Analyzer, analysistest.TestData(), "CheckCyclicFinalizer", "parity")
}

// The differences fixture holds the cases where the two could plausibly part, finalizers that start after `//line`
// directives naming a Go file or a grammar, where a port that wrote where the finalizer starts into the message any
// other way would show a finding one side makes and the other does not. It records no difference, since the two read
// the same calls and differ nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5005.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
