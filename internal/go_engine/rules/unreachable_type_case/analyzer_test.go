package unreachable_type_case

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4020"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for SA4020, copied unchanged from staticcheck/sa4020/testdata/<version>/<Package> into
// testdata/<version>/<Package>, hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
// Each version's folder holds the go.mod staticcheck's testutil lays over it, naming module example.com, whose path the
// notes' messages print in a type's name (example.com/CheckUnreachableTypeCases.T), and naming the version. SA4020
// reads no version; go1.18's package needs type parameters, which its module's version allows.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0", "go1.18")
}

// Over staticcheck's fixture modules and the house's parity module, the original and the port find the same findings.
// parity holds type switches over interfaces embedding others, aliases, receivers of both kinds, identical interfaces,
// a sealed interface of another package, nils and defaults in every place, type parameters on either side and a
// generated file, the messages printing the types.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4020.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "go1.18", "parity")
}

// The differences fixture holds the type switches whose types, printing or range the two could read apart. It records
// no difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4020.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
