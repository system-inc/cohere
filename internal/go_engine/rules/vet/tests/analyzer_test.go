package tests

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis/passes/tests"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackages are the fixture packages x/tools' own test runs, copied unchanged (byte for byte) into
// testdata/src with b, which b_x_test imports. a is loaded with its test variants, a_test among them, which looks up
// what its examples name through its import of a.
var upstreamPackages = []string{"a", "b_x_test", "divergent", "typeparams"}

// x/tools' own fixtures for tests, run as x/tools' test runs them, hold the port to every want the original expects,
// and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// Over x/tools' fixtures, parity's and dotted's, the last two holding every branch of the judgment and the shapes
// most likely to differ (parity's external test package among them, and dotted's test importing testing with a dot,
// whose names would clash with parity's), the original and the port find the same findings, each the same in every
// field.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, tests.Analyzer, Analyzer, analysistest.TestData(),
		append(slices.Clone(upstreamPackages), "parity", "dotted")...)
}

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: each finding of the original's that is false is dropped, each the port words differently because of it is
// renamed, and the true one the port alone makes where the original stopped too early is found, every record on its
// line with its reason on the line above, since the port is never worse than the original.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, tests.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
