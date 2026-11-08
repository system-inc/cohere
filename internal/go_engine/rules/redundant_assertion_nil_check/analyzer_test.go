package redundant_assertion_nil_check

import (
	"testing"

	"honnef.co/go/tools/simple/s1020"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for S1020, copied unchanged from simple/s1020/testdata/go1.0/
// CheckAssertNotNil into testdata/src/CheckAssertNotNil. The go1.0 folder sets the language version of staticcheck's
// run, which S1020 never reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
// It holds no golden file, since S1020 suggests no fix.
const upstream = "CheckAssertNotNil"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every
// shape the two queries branch on and the near miss of each, and a generated file, and their findings must be the
// same multiset, each the same to its end and its fixes. S1020 suggests no fix, so the ends are what this holds beside
// the starts, each if's header up to the end of its condition, and a fix either side made would be a difference.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1020.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the two could most plausibly part, an empty else the matcher takes
// for none on either if, labels and blocks around the inner if at more than one depth, an else if, an index or a
// selector asserted on, a function literal, a package that declares its own nil, and records none, since the port
// parts from the original nowhere. The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing,
// so the two are required to agree there finding for finding, ends and fixes too, in a call of its own.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1020.Analyzer, Analyzer, analysistest.TestData(), "differences",
		"differences/shadow")
}
