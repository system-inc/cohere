package unused_write

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/unusedwrite"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamIndexRenamed is the one finding of x/tools' fixtures the port makes in other words, a write to an array
// element: the original names its index as its form holds it, value and type, and the port as the source writes it.
// The fixture is copied unchanged, so the rename is given beside each call that reads it.
var upstreamIndexRenamed = parity.Record{
	File:   "a/unusedwrite.go",
	Line:   19,
	Text:   "renames: unused write to array index 1:int => unused write to array index 1",
	Reason: "the port names the index as written, 1, where the original prints the constant's value and type",
}

// x/tools' own fixtures for unusedwrite, copied unchanged into testdata/src, hold the port to every want the
// original expects, and to nothing more, its one want of an array element's write met through upstreamIndexRenamed.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.Renaming(upstreamIndexRenamed).RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a",
		"importsunsafe")
}

// Over x/tools' unsafe fixture and parity's, the second holding the shapes staticcheck's form builds differently, the
// original and the port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, unusedwrite.Analyzer, Analyzer, analysistest.TestData(), "importsunsafe", "parity")
}

// Over x/tools' main fixture the two find the same findings but upstreamIndexRenamed, given beside the call.
func TestPortMatchesOriginalOverUpstreamFixtureButTheIndexRenamed(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, unusedwrite.Analyzer, Analyzer, analysistest.TestData(), "a",
		upstreamIndexRenamed)
}

// Over the differences fixture the two disagree, and only where each disagreement is recorded there as adjudicated,
// as found by the port alone or as the same finding renamed.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, unusedwrite.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
