package redundant_canonical_header_key

import (
	"testing"

	"honnef.co/go/tools/simple/s1035"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for S1035, copied unchanged from
// simple/s1035/testdata/go1.0/CheckRedundantCanonicalHeaderKey into testdata/src/CheckRedundantCanonicalHeaderKey with
// its golden file. The go1.0 folder sets the language version of staticcheck's run, which S1035 never reads, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckRedundantCanonicalHeaderKey"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. The golden file holds no sections, so its comment is the whole fixed file.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the second holding every shape the query branches on and the near miss of
// each, a generated file, a dot and a renamed import, the original and the port find the same multiset, each finding
// the same to its end and its fixes. The fix's text is the inner call's argument as format.Node prints it on both
// sides, so this is where the two are held byte for byte, renamed and dot imports, arguments across lines and
// comments among them included. No fixture can load as net/http, whose own calls the type index never walks, so the
// port's guard for them is reasoned, not tested.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1035.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity",
		"parity/dotted")
}

// The differences fixture holds the shapes where the two could most plausibly part, a local that takes the name http,
// an embedding in a field, nested calls, a key across lines with a comment among it, and a generic function, and
// records none, since the port parts from the original nowhere. RequireOnlyAdjudicated fails a fixture that records
// nothing, so the two are required to agree there finding for finding, fixes and all, in a call of its own, which
// fails when the original finds nothing there.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1035.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
