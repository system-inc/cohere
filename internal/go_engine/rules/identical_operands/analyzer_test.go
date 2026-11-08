package identical_operands

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa4000"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackages are staticcheck's own fixtures for SA4000, copied unchanged from its testdata/go1.0, go1.18 and
// go1.22 into testdata/src under directories of the same names, each holding a package of one name, so the version
// folder is the first element of each import path. The versions set the language of staticcheck's runs, which SA4000
// never reads beyond what the files need, generics and math/rand/v2, which the toolchain's own version allows. The
// go1.0 package imports "C", so loading it runs cgo, whose generated `0 == 0` the original spares.
var upstreamPackages = []string{
	"go1.0/CheckLhsRhsIdentical", "go1.18/CheckLhsRhsIdentical", "go1.22/CheckLhsRhsIdentical",
}

// staticcheck's fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds every
// operator, every kind of type the check tells floats apart from, the spared random calls, the compile-time assertion
// of comparability and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4000.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, []string{"parity"})...)
}

// The differences fixture holds the expressions where the two could most plausibly part: a comment inside an operand,
// type parameters of structs, and line directives in a file generated and one not. It records no difference, since
// the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4000.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
