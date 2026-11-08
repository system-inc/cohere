package invalid_struct_tag

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa5008"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// vendored loads a fixture module as staticcheck's testutil does, imports read from the module's vendor folder. go1.0
// and flags name Go 1.0, older than the 1.14 that reads a vendor folder by default, so without the flag go-flags is
// looked for in the module cache, which a load with nothing fetched never fills. The harness sets no `GOFLAGS`, so the
// flag is given as a target's variable; it chooses how imports are found, not the platform, and changes nothing else
// of the load.
var vendored = parity.On("GOFLAGS=-mod=vendor")

// staticcheck's own fixtures for SA5008, copied unchanged from staticcheck/sa5008/testdata/<version>/<Package> into
// testdata/<version>/<Package>, go1.0's vendor folder with them: its one-line stand-in for
// github.com/jessevdk/go-flags, which the first package imports and which a module's zip, and so the vendored copy of
// staticcheck, leaves out. Each version's folder holds the go.mod staticcheck's testutil lays over it, naming module
// example.com, which an XML message names a type by, and naming the version; a module older than Go 1.14 reads its
// vendor folder with no modules.txt. SA5008 reads no version; go1.18's package needs type parameters, which its
// module's version allows. They hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	vendored.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0", "go1.18")
}

// Over staticcheck's fixture modules and the house's, the original and the port find the same findings, the end of
// each held as well as its start. The house's are parity, holding every way the original accepts or refuses a tag,
// and flags, a package importing go-flags beside one that does not, through a vendor folder as go1.0's. Neither holds
// a package whose path is encoding/json, where the original leaves `json` values alone, since a module's package of
// that path is ambiguous with the standard library's and does not load.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	vendored.RequireIdentical(test, sa5008.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "go1.18", "parity",
		"flags")
}

// The differences fixture holds the tags whose reading the two could part on, several keys at once, several names to
// a line and a nested struct. It records no difference, since there is none, so the two must agree there finding for
// finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	vendored.RequireIdentical(test, sa5008.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
