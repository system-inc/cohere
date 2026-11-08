package testing_goroutine

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/testinggoroutine"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// The original's test turns its experimental subtest flag on before anything runs, so the fixtures' subtest wants are
// reported, and these tests do the same, for the original and the port alike.
// TestPortMatchesOriginalWithTheSubtestFlagOff turns both off for its own run, and back on. The harness runs the two
// analyzers as it is handed them, so each tier runs under the flag as set here.
func init() {
	setSubtest("true")
}

// setSubtest sets the subtest flag of the original and the port to value.
func setSubtest(value string) {
	for _, analyzer := range []*analysis.Analyzer{testinggoroutine.Analyzer, Analyzer} {
		if setError := analyzer.Flags.Set("subtest", value); setError != nil {
			// Invariant: both declare subtest as a bool flag, which takes "true" and "false".
			panic(setError)
		}
	}
}

// x/tools' own fixtures for testinggoroutine, copied unchanged into testdata/src, hold the port to every want the
// original expects with the subtest flag on, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// Over x/tools' fixtures, parity's, and withouttesting's, the second holding every branch of the judgment and the
// shapes most likely to differ, and the third a package that takes a test without importing testing, the original
// and the port find the same findings, each the same in every field, with the subtest flag on.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	requireIdentical(test)
}

// The same comparison with the subtest flag off, its default and how cohere runs the pass, so the port drops the
// subtest findings the original drops, and keeps every other.
//
// Not parallel: the flag is the analyzers' own, which every other test here reads on, so this test runs alone,
// before any parallel test resumes, and turns the flag back on before it ends.
func TestPortMatchesOriginalWithTheSubtestFlagOff(test *testing.T) {
	setSubtest("false")
	defer setSubtest("true")
	requireIdentical(test)
}

// requireIdentical holds the original and the port to the same findings over every fixture package but the
// differences, under the subtest flag as it is set.
func requireIdentical(test *testing.T) {
	test.Helper()
	parity.RequireIdentical(test, testinggoroutine.Analyzer, Analyzer, analysistest.TestData(), "a", "typeparams",
		"parity", "withouttesting")
}

// Over the differences fixture, with the subtest flag on, the two disagree only where each disagreement was
// adjudicated in the port's favour, each record with its reason: a go statement's call arguments and a t.Run's name,
// which the goroutine executing the statement evaluates, judged with the region enclosing it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, testinggoroutine.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
