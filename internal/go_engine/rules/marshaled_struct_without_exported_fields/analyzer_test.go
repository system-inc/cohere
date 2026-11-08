package marshaled_struct_without_exported_fields

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa9005"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA9005, copied unchanged from staticcheck/sa9005/testdata/go1.0/CheckNoopMarshal into
// testdata/go1.0/CheckNoopMarshal, holds the port to every `//@ diag(...)` note the original expects, and to nothing
// more. The go1.0 folder holds a go.mod naming the module and the version staticcheck's testutil lays over the folder
// as it runs it, since the messages name each struct by its package's import path, example.com/CheckNoopMarshal.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0")
}

// Over staticcheck's fixture module and the house's two, the original and the port find the same findings. The parity
// module holds every function judged and the shapes staticcheck's representation builds an argument or a call in more
// than one way, and the generated module holds generated files and line directives pointing across.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9005.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "parity", "generated")
}

// The differences module holds the calls where staticcheck_call takes another path than callcheck, or could. It
// records no difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9005.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
