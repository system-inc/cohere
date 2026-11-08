package uncalled_deferred_function

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa9010"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamFixtures are staticcheck's own fixtures for SA9010, copied unchanged from staticcheck/sa9010/testdata into
// testdata/src with their version folders kept, since both versions hold a package named deferr: go1.0's at
// example.com/deferr, and go1.18's, whose calls instantiate a generic function. A version folder sets the language
// version of staticcheck's run, which SA9010 never reads, and a fixture loaded outside a module may use generics, so
// each loads from a tree laid out by import path as the house's fixtures do, the folder part of its path.
var upstreamFixtures = []string{"go1.0/example.com/deferr", "go1.18/deferr"}

// staticcheck's own fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamFixtures...)
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds defers of
// calls that return a function through every route a function is reached by, named function types, generic results,
// method values and expressions, builtins, calls returning nothing, a value or several, and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9010.Analyzer, Analyzer, analysistest.TestData(),
		append(slices.Clone(upstreamFixtures), "parity")...)
}

// The differences fixture holds the defers where the two could most plausibly part: a call returning a type parameter
// constrained to functions, a call returning an interface a function satisfies, and a method value deferred whole. It
// records no difference, since the port parts from the original nowhere, so the two must agree there finding for
// finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9010.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
