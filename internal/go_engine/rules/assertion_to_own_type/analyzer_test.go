package assertion_to_own_type

import (
	"testing"

	"honnef.co/go/tools/simple/s1040"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for S1040, copied unchanged from
// simple/s1040/testdata/go1.0/CheckSameTypeTypeAssertion into testdata/src/CheckSameTypeTypeAssertion. It has no
// golden file, since S1040 suggests no fix. The go1.0 folder sets the language version of staticcheck's run, which
// S1040 never reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckSameTypeTypeAssertion"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the second holding every shape the judgment branches on and the near miss
// of each, values and types written with parentheses, over several lines and with comments, and a generated file, the
// original and the port find the same multiset, each finding the same to its end and its fixes. S1040 suggests no
// fix, so this holds the ends beyond the start and message, which span the whole assertion.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1040.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the two could most plausibly part, an interface shadowed in an inner
// scope, embedded interfaces against the literal of their methods, nested generic instantiations, an assertion in a
// closure and as an argument, and an alias of the universe's error, and records none, since the port parts from the
// original nowhere. RequireOnlyAdjudicated fails a fixture that records nothing, so the two are required to agree
// there as everywhere else, in a call of its own, which fails when the original finds nothing there, so the agreement
// is over findings.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1040.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
