package nil_context_argument

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa1012"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamFixtures are staticcheck's own fixtures for SA1012, copied unchanged from staticcheck/sa1012/testdata into
// testdata/src with their golden files and their version folders kept, since both hold a package of one name: go1.0's,
// and go1.18's, whose calls instantiate generic functions. A version folder sets the language version of staticcheck's
// run, which SA1012 never reads, and a fixture loaded outside a module may use generics, so each loads from a tree laid
// out by import path as the house's fixtures do, the folder part of its path.
var upstreamFixtures = []string{"go1.0/checkStdlibUsageNilContext", "go1.18/checkStdlibUsageNilContext"}

// The upstream fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamFixtures...)
}

// The port's fixes make each upstream fixture's golden file, a section for each fix's message, as staticcheck's
// testutil requires of the original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstreamFixtures...)
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings, fixes included. parity
// holds a nil context passed to functions, methods, promoted and interface methods, generic functions and the standard
// library's, through parentheses and instantiations, and the calls the original leaves alone, conversions, function
// variables, a method expression with no parameters and a shadowed nil, and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1012.Analyzer, Analyzer, analysistest.TestData(),
		append(slices.Clone(upstreamFixtures), "parity")...)
}

// The differences fixture holds the shapes where the two could most plausibly part, and records none, since the port
// parts from the original nowhere, so the two must agree there finding for finding, fixes included. It holds the
// original's false positive, a method expression whose receiver is the nil, and parameters a context only in part: an
// alias, an embedding interface and a constrained type parameter.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1012.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
