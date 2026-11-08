package slog

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/slog"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for slog, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "b")
}

// Over x/tools' fixtures and parity's, the second holding every entry of the table of slog's functions, every branch
// of the judgment and the shapes most likely to differ, the original and the port find the same findings, each the
// same in every field.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, slog.Analyzer, Analyzer, analysistest.TestData(), "a", "b", "parity")
}

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour, each record with its reason on the line above it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, slog.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
