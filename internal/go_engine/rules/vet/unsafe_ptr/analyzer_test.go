package unsafe_ptr

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/unsafeptr"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for unsafeptr, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, unsafeptr.Analyzer, Analyzer, analysistest.TestData(), "a", "typeparams", "parity")
}

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: every finding the original alone makes there is dropped on its line, with the reason it is wrong on the
// line above, since the port is never worse than the original.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, unsafeptr.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
