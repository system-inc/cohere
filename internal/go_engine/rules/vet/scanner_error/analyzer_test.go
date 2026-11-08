package scanner_error

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/scannererr"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixture for scannererr, package `a`, copied unchanged into testdata/src, holds the port to every want
// the original expects, and to nothing more. x/tools runs it through RunWithSuggestedFixes, but neither side offers a
// fix, so there is no golden file to compare; that the port offers none either, as the original offers none, is
// what TestPortMatchesOriginalFindingForFinding holds, comparing every finding's fixes over the same package.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a")
}

// Over x/tools' fixture and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field. The port differs from the
// original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, scannererr.Analyzer, Analyzer, analysistest.TestData(), "a", "parity")
}
