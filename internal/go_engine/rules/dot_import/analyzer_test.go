package dot_import

import (
	"testing"

	"honnef.co/go/tools/stylecheck/st1001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for `ST1001`, copied unchanged from stylecheck/st1001/testdata/go1.0 into
// testdata/go1.0, hold the port to every `//@ diag(...)` note the original expects, and to nothing more. The folder
// holds the go.mod staticcheck's testutil lays over it, `module example.com` at Go 1.0. The fixture's test file is in
// the package itself, so the package's file is in both the package and its test variant and its one note is met in
// each.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0")
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds every
// import shape the check reads and every near miss, in a package under no staticcheck.conf and in one under a
// staticcheck.conf allowing some paths. The port differs from the original only for a project under the user's cache
// directory, whose staticcheck.conf the original never reads and the port does (#sc5qmy7, see the package's comment),
// and no fixture sits there, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1001.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "parity/...")
}
