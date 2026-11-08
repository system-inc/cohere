package defers

import (
	"testing"

	upstream "golang.org/x/tools/go/analysis/passes/defers"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixture for defers, copied unchanged into testdata/src, holds the port to every want the original
// expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a")
}

// parity's wants are ours, each one what the original reports there, so the port is held to them as to upstream's,
// by the same reading of `// want` comments. The differential below already holds the port to the original over
// parity, and this holds both to what a reader of the fixture expects, so the two cannot agree on a wrong answer
// unseen.
func TestPortMatchesParityWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "parity")
}

// Over x/tools' fixture and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field. The port differs from the
// original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, upstream.Analyzer, Analyzer, analysistest.TestData(), "a", "parity")
}
