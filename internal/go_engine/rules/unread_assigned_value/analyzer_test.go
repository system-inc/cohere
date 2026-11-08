package unread_assigned_value

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4006"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// Staticcheck's own fixture for SA4006, copied unchanged from its testdata/go1.0 into
// testdata/src/CheckUnreadVariableValues, holds the port to every `//@ diag(...)` note the original expects, and to
// nothing more. The fixture uses nothing newer than Go 1.0, so it needs no version of its own. Its test file, with a
// test and an example, loads as the package's in-package tests.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckUnreadVariableValues")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds the shapes
// the check depends on, which assignments and increments have a value of their own, what reads such a value through
// phis and switch tags, the forms that assign several values at once, a file goyacc generated and a file another tool
// did.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4006.Analyzer, Analyzer, analysistest.TestData(), "CheckUnreadVariableValues",
		"parity")
}

// The differences fixture holds the cases where the two could plausibly disagree: values passing through loops of
// phis that reach each other, and switches nested in closures. It records no difference, since the two read the same
// form and differ nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4006.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
