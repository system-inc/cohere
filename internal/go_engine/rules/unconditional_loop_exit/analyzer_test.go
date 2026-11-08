package unconditional_loop_exit

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4004"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for SA4004, copied unchanged from staticcheck/sa4004/testdata/<version>/<Package> into
// testdata/<version>/<Package>, hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
// Each version's folder holds the go.mod staticcheck's testutil lays over it, naming module example.com and the
// version, so each loads as the module of its version. SA4004 reads no version; go1.18's package needs type
// parameters, which its module's version allows.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0", "go1.18")
}

// Over staticcheck's fixture modules and the house's parity module, the original and the port find the same findings,
// repeats counted, since the original reports a loop in a function literal once for each function walking it. parity
// holds every exit and branching statement the check reads, labels naming this loop and another, gotos and continues
// at every depth, every kind of ranged type and type parameter, nested loops and literals, a generated file, and the
// loops the check leaves alone.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4004.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "go1.18", "parity")
}

// The differences fixture holds the loops whose labels or whose nesting in function literals the two could read
// apart. It records no difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4004.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
