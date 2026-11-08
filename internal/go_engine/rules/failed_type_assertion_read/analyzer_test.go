package failed_type_assertion_read

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa9008"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackage is staticcheck's own fixture for SA9008, copied unchanged from its testdata/go1.0 into
// testdata/src. The fixture uses nothing newer than Go 1.0, so it needs no version of its own.
const upstreamPackage = "CheckTypeAssertionShadowingElse"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackage)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, related places included.
// parity holds the shapes the check depends on, the forms the pattern accepts and refuses and the functions an if
// statement can be in.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9008.Analyzer, Analyzer, analysistest.TestData(), upstreamPackage, "parity")
}

// The differences fixture holds the places where a function's syntax and the functions staticcheck builds part:
// literals nested deep, literals in dead code, literals staticcheck never builds because the expression around them
// is a constant, and a method named _. It records no difference, since the two read the same form and differ nowhere,
// so they must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9008.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
