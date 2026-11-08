package signal_channel_analyzer

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/sigchanyzer"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixture for sigchanyzer, a.go and its golden a.go.golden, copied unchanged into testdata/src and run as
// x/tools' test runs it, with suggested fixes and no flags: every `// want` the original expects, the port reports at
// the same line with the same message, analysistest fails on a finding no want expects as much as on a want nothing
// found, and every fix the port offers, applied, turns a.go into the golden.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, analysistest.TestData(), "a")
}

// Over x/tools' fixture and parity's, the second holding every branch of the judgment and of the fix and the shapes
// most likely to differ, the original and the port find the same findings, each the same in every field, its
// suggested fixes among them.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sigchanyzer.Analyzer, Analyzer, analysistest.TestData(), "a", "parity")
}

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour, each record with its reason on the line above it or in its function's doc. Every edit of the fixes of a
// finding only the port makes is a `fixes at` record on that finding's line, naming the make call it rewrites, often
// in another file, and the text it puts there, and every such record is met by one.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, sigchanyzer.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
