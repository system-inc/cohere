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
	parity.RequireStaticcheckWants(test, pushAnalyzer, analysistest.TestData(), "CheckEvenSliceLength")
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
	parity.RequireIdenticalFacts(test, sa5012.Analyzer, pushAnalyzer, analysistest.TestData(), "CheckEvenSliceLength",
		"parity", "parity/pairs")
}

// A finding never turns on the order a package declares its functions in (#8d9r2rh). Packages order/forward and
// order/back hold one cycle of calls in two orders, a walk in declaration order entering it at another function in
// each, and a slice of one passed into the cycle reaches its length check in both, so the port reports it in both.
// The original reports it in order/back, where the two are held identical and to the fixture's own note, and misses
// it in order/forward, which records the finding the port alone makes there.
func TestACycleReportsTheSameInEitherDeclarationOrder(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "order/back")
	parity.RequireIdentical(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "order/back")
	parity.RequireOnlyAdjudicated(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "order/forward")
}

// A finding never turns on whether a package declares a function before or after one that reaches it through a
// stand-in (#8d9r2rh). Packages standin/first and standin/last hold the same functions, with no cycle of calls and no
// function calling itself: three callers each pass a slice to a function of no package, an instantiation, a bound
// method and a thunk, that stands for one of the package's own, which passes it on to a length check. first declares
// the callers before what they reach and last after, and a slice of one passed to each caller reaches the check in
// both, so the port reports the three in both. The original reports them in standin/last, where the two are held
// identical and to the fixture's own notes, and misses them in standin/first, which records the three findings the
// port alone makes there.
func TestAStandInReportsTheSameInEitherDeclarationOrder(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "standin/last")
	parity.RequireIdentical(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "standin/last")
	parity.RequireOnlyAdjudicated(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "standin/first")
}

// A mark goes around a cycle of calls as often as the passes carry it (#8d9r2rh). Package around holds a function
// calling itself with its slices swapped, and two functions passing theirs back and forth, where a slice reaches the
// length check only a call or two on. The original's walk visits each function once and misses those in every order a
// package can declare them in, so the fixture records each finding the port alone makes, beside one both make.
func TestAMarkGoesAroundACycleAsOftenAsItsPassesCarryIt(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "around")
}

// A finding never turns on which generic function was instantiated first (#8d9r2rh). Packages instance/first and
// instance/last hold three generic functions and a slice of one passed to each, with the one that checks its slice
// and the one that checks nothing declared and instantiated in either order. The port asks the mark of the declared
// function's own parameter, and finds in both the slices passed to the two functions that check theirs. The original
// reads the mark off the instantiation's signature, which two instantiations of identical signatures share, and each
// package records where that parts the two: a slice it reports by another function's check, dropped, a finding it
// makes under another function's parameter name, renamed, and the slice it misses, found.
func TestAnInstantiationReportsByItsDeclaredFunctionInEitherOrder(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "instance/first")
	parity.RequireOnlyAdjudicated(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "instance/last")
}

// The differences fixture holds the calls where the two could most plausibly part, a slice whose length depends on
// the path taken, one reaching the call through a loop's phi, arguments spread over lines and slices of slices of an
// array. It records no difference, since the port parts from the original nowhere it can run, so the two must agree
// there finding for finding and mark for mark.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "differences")
	parity.RequireIdenticalFacts(test, sa5012.Analyzer, pushAnalyzer, analysistest.TestData(), "differences")
}

// The one place the two part: a go or defer statement passing an odd slice to a marked parameter, whose call's
// source is the statement, which the original reads as a call expression and panics on, losing the package's every
// finding. The port reports each odd argument inside the statement's call, the two the fixture's go and defer pass
// and records, and not the even one.
func TestPortReportsWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	parity.RequirePortWhereOriginalPanics(test, sa5012.Analyzer, Analyzer, analysistest.TestData(), "statements")
}
