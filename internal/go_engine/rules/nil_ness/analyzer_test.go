package nil_ness

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/nilness"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for nilness, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "b", "c", "d")
}

// Over x/tools' fixtures and parity's, the second holding the shapes staticcheck's form builds differently, the
// original and the port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, nilness.Analyzer, Analyzer, analysistest.TestData(), "a", "b", "c", "d", "parity")
}

// Over the differences fixture the two disagree, and only where each disagreement is recorded there as adjudicated.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, nilness.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
