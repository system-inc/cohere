package range_over_string_directly

import (
	"slices"
	"testing"

	"honnef.co/go/tools/simple/s1029"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackages is staticcheck's own fixtures for S1029, copied unchanged from simple/s1029/testdata/go1.0 and
// testdata/go1.18, each into testdata/src under its version folder, since both are the folder CheckRangeStringRunes.
// The version folder sets the language version of staticcheck's run, which S1029 never reads, so the fixtures load
// from a tree laid out by import path, with the toolchain's version, which the generic one needs and the other does
// not mind. They hold no golden file, since S1029 suggests no fix.
var upstreamPackages = []string{"go1.0/CheckRangeStringRunes", "go1.18/CheckRangeStringRunes"}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under SA6003's name, or any other, as readily as under S1029's; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1029.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1029.Analyzer.Name)
	}
}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// Over staticcheck's fixtures and parity's, the second holding every shape the check branches on and the near miss
// of each, and a generated file, which neither filters, the original and the port find the same multiset, each
// finding the same to its end and its fixes. The original reports with the pass's Reportf, which gives a finding no
// end and no fix, so an end or a fix the port gave would be a difference here.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1029.Analyzer, Analyzer, analysistest.TestData(),
		append(slices.Clone(upstreamPackages), "parity")...)
}

// The differences fixture holds the shapes where the two could most plausibly part, ranges several function
// literals deep, in a package variable's initializer, in a method of a generic type and in a generic literal, and
// records none, since the port parts from the original nowhere. RequireOnlyAdjudicated fails a fixture that records
// nothing, so the two are required to agree there as everywhere else, in a call of its own, which fails when the
// original finds nothing there, so the agreement is over findings.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1029.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
