package iface_assert

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/ifaceassert"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for ifaceassert, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and of the search for a free
// type parameter, and the shapes most likely to differ, the original and the port find the same findings, each the
// same in every field. parity/other is imported by parity, not judged itself. The port differs from the original
// nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, ifaceassert.Analyzer, Analyzer, analysistest.TestData(), "a", "typeparams", "parity",
		"parity/dotted")
}
