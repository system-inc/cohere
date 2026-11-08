package wait_group

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/waitgroup"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixture for waitgroup, package `a`, copied unchanged into testdata/src, holds the port to every want
// the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a")
}

// Over x/tools' fixture and parity's with its neighbours, which hold every branch of the judgment and the shapes most
// likely to differ, the original and the port find the same findings, each the same in every field. The port differs
// from the original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, waitgroup.Analyzer, Analyzer, analysistest.TestData(), "a", "parity", "dotted",
		"counted", "indirect")
}
