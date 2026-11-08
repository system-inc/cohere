package ineffective_bitwise_operation

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4016"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream are staticcheck's own fixtures for SA4016, copied unchanged from
// staticcheck/sa4016/testdata/<version>/<Package> into testdata/<version>/<Package>. Each version's folder holds the
// go.mod staticcheck's testutil lays over it, naming module example.com, which the dot-importing test of go1.0 imports
// by path, and naming the version, so each loads as the module of its version. SA4016 reads no version; go1.18's
// package needs type parameters, which its module's version allows.
var upstream = []string{"go1.0", "go1.18"}

// staticcheck's own fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream...)
}

// Over staticcheck's fixture modules and the house's parity module, the original and the port find the same findings,
// the messages printing the operands. parity holds zeros written every way an integer literal can be, constants
// declared as iota alone, in groups, typed, inside a function and in another file, and the operations and constants
// the check leaves alone.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4016.Analyzer, Analyzer, analysistest.TestData(), append(upstream, "parity")...)
}

// The differences fixture holds the operations whose printing or whose constant's declaration the two could read
// apart. It records no difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4016.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
