package misplaced_error_result

import (
	"testing"

	"honnef.co/go/tools/stylecheck/st1008"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for `ST1008`, copied unchanged from stylecheck/st1008/testdata/<version> into
// testdata/<version>, hold the port to every `//@ diag(...)` note the original expects, and to nothing more. Each
// version's folder holds the go.mod staticcheck's testutil lays over it, `module example.com` at that version, and
// go1.9's holds an alias of error, which needs that version.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0", "go1.9")
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds every
// arrangement of results the check reads and every function the original's builder builds or leaves unbuilt, the port
// listing them from the syntax where the original lists them from the intermediate representation. The port differs
// from the original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1008.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "go1.9", "parity")
}
