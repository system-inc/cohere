package appends

import (
	"testing"

	// The original shares this package's name, and this package declares no identifier appends, so in this file
	// appends.Analyzer is always x/tools' and the bare Analyzer always the port.
	"golang.org/x/tools/go/analysis/passes/appends"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for appends, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "b")
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field. The port differs from the
// original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, appends.Analyzer, Analyzer, analysistest.TestData(), "a", "b", "parity")
}
