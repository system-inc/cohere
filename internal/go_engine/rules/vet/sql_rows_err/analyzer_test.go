package sql_rows_err

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/sqlrowserr"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixture for sqlrowserr, copied unchanged into testdata/src, holds the port to every want the original
// expects, and to nothing more. x/tools runs it through RunWithSuggestedFixes, but neither side offers a fix and the
// fixture has no golden file, so that run checks nothing a plain one does not, and a fix the port alone offered is a
// difference RequireIdentical names.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a")
}

// Over x/tools' fixture and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field. The port differs from the
// original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sqlrowserr.Analyzer, Analyzer, analysistest.TestData(), "a", "parity")
}
