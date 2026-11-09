package modified_writer_buffer

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1023"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for SA1023, copied unchanged from its testdata/go1.0 and testdata/go1.9 into
// testdata/src under directories of the same names, hold the port to every `//@ diag(...)` note the original expects,
// and to nothing more. Staticcheck's runner loads each version directory as a module of its own, and the two declare
// the same types, so they stay two packages; the second needs type aliases, which the toolchain's own version, newer
// than both, allows.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0/CheckWriterBufferModified",
		"go1.9/CheckWriterBufferModified")
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds the
// shapes the check depends on.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1023.Analyzer, Analyzer, analysistest.TestData(), "go1.0/CheckWriterBufferModified",
		"go1.9/CheckWriterBufferModified", "parity")
}

// The differences fixture holds the cases where the port and the original could plausibly disagree. It records no
// difference, since our form is built as buildir builds its own and the two differ nowhere, so they must agree there
// finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1023.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
