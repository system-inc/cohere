package shift

import (
	"testing"

	upstream "golang.org/x/tools/go/analysis/passes/shift" // The original, named apart from the port's package.

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for shift, copied into testdata/src, hold the port to every want the original expects, and to
// nothing more. a/a.go is copied unchanged. typeparams/typeparams.go is gofmt'd, as every Go file cohere owns must be,
// which changed the spacing within six of its lines and moved none, so its wants name the lines they named.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and of the dead code detector,
// type parameters with one width and with several, and the shapes most likely to differ, the original and the port
// find the same findings, each the same in every field. The end matters here, since both report the whole shift's
// range, and a port that reported the shifted operand's range alone would start at the same place. The port differs
// from the original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, upstream.Analyzer, Analyzer, analysistest.TestData(), "a", "typeparams", "parity")
}
