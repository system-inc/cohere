package impossible_type_assertion

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa5010"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for SA5010, copied unchanged from its testdata/go1.0 and testdata/go1.18 into
// testdata/src/go1.0 and testdata/src/go1.18, its version folders kept as the first element of each import path, since
// the two packages share a name, hold the port to every `//@ diag(...)` note the original expects, and to nothing
// more. Nothing gates SA5010 by version, and the loader type-checks both at the newest version, which the go1.18
// fixture's generics need.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0/CheckImpossibleTypeAssertion",
		"go1.18/CheckImpossibleTypeAssertion")
}

// Over staticcheck's fixtures and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings, the end of the assertion's syntax held as well as its start.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5010.Analyzer, Analyzer, analysistest.TestData(),
		"go1.0/CheckImpossibleTypeAssertion", "go1.18/CheckImpossibleTypeAssertion", "parity")
}

// The differences fixture holds the cases where the two could plausibly part, assertions to interfaces of many
// methods, with contradictions before and after a generic one, where a judgment that hung on the order of the
// methods would show a finding or a message one side makes and the other does not. It records no difference, since
// the two read the same method sets and differ nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5010.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
