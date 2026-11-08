package lost_cancel

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/lostcancel"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for lostcancel, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "b", "typeparams")
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment, every path shape the search
// walks, and the shapes most likely to differ, the original and the port find the same findings, each the same in
// every field. parity imports parity/halt, so ctrlflow learns from a dependency which of its functions never return,
// and parity/command is a package main. parity's end_of_file.go holds the return cfg makes at a closing brace whose
// end overflows the file, which both sides clip to where it starts.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(
		test, lostcancel.Analyzer, Analyzer, analysistest.TestData(),
		"a", "b", "typeparams", "parity", "parity/command",
	)
}

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: every finding the original makes there is wrong, and the port drops it, each record with its reason on the
// line above it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, lostcancel.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
