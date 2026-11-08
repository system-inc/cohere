package inconsistent_receiver_name

import (
	"testing"

	"honnef.co/go/tools/stylecheck/st1016"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for `ST1016`, copied unchanged from stylecheck/st1016/testdata/go1.0 into
// testdata/go1.0, hold the port to every `//@ diag(...)` note the original expects, and to nothing more. The folder
// holds the go.mod staticcheck's testutil lays over it, `module example.com` at Go 1.0.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0")
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds every way
// the methods of one type name their receivers and every near miss, the port listing its types from the syntax where
// the original lists them from the intermediate representation. The port differs from the original nowhere, so there
// is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1016.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "parity")
}
