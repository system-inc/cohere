package http_response

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/httpresponse"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for httpresponse, copied unchanged into testdata/src, hold the port to every want the
// original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// Over x/tools' fixtures, parity's and indirect's, parity holding every branch of the judgment and the shapes most
// likely to differ, and indirect a package the pass skips for not importing net/http, the original and the port find
// the same findings, each the same in every field.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(
		test, httpresponse.Analyzer, Analyzer, analysistest.TestData(), "a", "typeparams", "parity", "indirect",
	)
}

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: every finding the original makes there is a false positive the port drops, each record with its reason on
// the line above it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, httpresponse.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
