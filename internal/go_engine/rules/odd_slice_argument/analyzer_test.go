package odd_slice_argument

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa5012"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA5012, copied unchanged from staticcheck/sa5012/testdata/go1.0/CheckEvenSliceLength
// into testdata/src/CheckEvenSliceLength, holds the port to every `//@ diag(...)` note the original expects, and every
// `//@ fact(...)` note, the mark on the parameter it names, and to nothing more. The go1.0 folder sets the language
// version of staticcheck's run, which SA5012 never reads, so the fixture loads from the tree laid out by import path.
// Its strings.NewReplacer note holds the facts across packages, since the mark comes from the port's run on strings.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckEvenSliceLength")
}

// Over staticcheck's fixture and parity's packages, the original and the port find the same findings. parity holds
// every shape of the length check, either way around, checks that mark nothing because they do not dominate every
// return, do not panic, do not divide by two or do not read a length, chains of passing a slice on, sometimes and on
// every path, methods and a method expression, a function of another package, parity/pairs, and every way a slice's
// length is known or is not, with a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "CheckEvenSliceLength", "parity",
		"parity/pairs")
}

// Over the same fixtures, the marks the original and the port export are the same, on the fixture packages and on
// every package they import, strings among them, whose mark on NewReplacer's oldnew the NewReplacer finding stands on.
func TestPortExportsTheOriginalsFacts(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalFacts(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "CheckEvenSliceLength",
		"parity", "parity/pairs")
}

// The differences fixture holds the calls where the two could most plausibly part, a slice whose length depends on
// the path taken, one reaching the call through a loop's phi, arguments spread over lines and slices of slices of an
// array. It records no difference, since the port parts from the original nowhere it can run, so the two must agree
// there finding for finding and mark for mark.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "differences")
	parity.RequireIdenticalFacts(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The one place the two part: a go or defer statement passing an odd slice to a marked parameter, whose call's
// source is the statement, which the original reads as a call expression and panics on, losing the package's every
// finding. The port reports each odd argument inside the statement's call, the two the fixture's go and defer pass
// and records, and not the even one.
func TestPortReportsWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	parity.RequirePortWhereOriginalPanics(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "statements")
}
