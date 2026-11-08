package byte_slice_write_string

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa6006"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA6006, copied unchanged from
// staticcheck/sa6006/testdata/go1.0/CheckByteSliceInIOWriteString into testdata/go1.0/CheckByteSliceInIOWriteString,
// whose folder holds a go.mod naming Go 1.0, the one staticcheck's testutil lays over it as it runs it, since the
// check reads a file's language version: from Go 1.18 a slice of a type defined on byte converts to a string too.
// Each folder of testdata named here holds a go.mod, so each loads as a module of its own.
const upstream = "go1.0"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and the house's two parity modules, the one older than Go 1.18 with a file tagged newer
// and the one newer with a file tagged older, holding the shapes the pattern accepts and refuses and the slices that
// do and do not convert, the original and the port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6006.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity",
		"parity_current")
}

// The differences fixture holds the calls where the port's walk and match could plausibly part from the pattern's,
// and records no difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6006.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
