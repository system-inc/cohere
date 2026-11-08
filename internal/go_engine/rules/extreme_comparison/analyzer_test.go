package extreme_comparison

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa4003"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackages are staticcheck's own fixtures for SA4003, copied unchanged from its testdata/go1.0 and
// testdata/go1.18 into testdata/src under directories of the same names. Staticcheck's runner loads each version
// directory as a module of its own, and the two hold a package of one name, so the version folder is the first element
// of each import path; the second needs type parameters, which the toolchain's own version, newer than both, allows.
// SA4003 reads no version.
var upstreamPackages = []string{"go1.0/CheckExtremeComparison", "go1.18/CheckExtremeComparison"}

// staticcheck's own fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds every
// integer kind compared with its limits by name and as literals, from both sides and under every operator, type
// parameters with and without one core type, and a generated file the original filters.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4003.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, []string{"parity"})...)
}

// The differences fixture holds the cases where the two could plausibly disagree. It records no difference, since the
// two read the same syntax and differ nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4003.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
