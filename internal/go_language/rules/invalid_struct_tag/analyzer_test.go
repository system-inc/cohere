package invalid_struct_tag

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/staticcheck/sa5008"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
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

// escapedSource is the escaped fixture's one Go file, escaped.go. It imports go-flags spelled with an escape,
// `go\x2dflags`, and the escape is what the fixture tests, so the file is never kept in testdata: gofmt canonicalizes
// an import path, rewriting it to `go-flags`, and internal/testing/guard holds every Go file the module owns, testdata
// included, to gofmt, as the release's first step does.
const escapedSource = `// Package escaped imports go-flags spelled with an escape, which names the same path, so the
// keys go-flags repeats are left alone in it, and every other key repeated is still found.
package escaped

import _ "github.com/jessevdk/go\x2dflags"

type options struct {
	// The original compares the import's raw text, reads no go-flags import here, and reports the key go-flags repeats
	// on purpose.
	A []int ` + "`default:\"foo\" default:\"bar\"`" + ` // port drops: duplicate struct tag "default"
	B int   ` + "`long:\"foo\" long:\"bar\"`" + `
}
`

// escapedTestdata is a testdata of the test's own holding the escaped fixture: testdata/escaped's go.mod and vendor
// folder, copied, and escaped.go written beside them from escapedSource.
func escapedTestdata(test *testing.T) string {
	test.Helper()
	testdata := test.TempDir()
	folder := filepath.Join(testdata, "escaped")
	fixture := filepath.Join(analysistest.TestData(), "escaped")
	if copyError := os.CopyFS(folder, os.DirFS(fixture)); copyError != nil {
		test.Fatalf("copying %s: %v", fixture, copyError)
	}
	written := filepath.Join(folder, "escaped.go")
	if writeError := os.WriteFile(written, []byte(escapedSource), 0o600); writeError != nil {
		test.Fatalf("writing %s: %v", written, writeError)
	}
	return testdata
}

// The differences fixture holds the tags whose reading the two could part on, several keys at once, several names to
// a line and a nested struct. It records no difference, since there is none, so the two must agree there finding for
// finding. The escaped fixture holds the one place they part: a package importing go-flags spelled with an escape,
// `go\x2dflags`, whose repeated `default` key the original reports, comparing the import's raw text, and the port
// rightly leaves alone, reading its path (#dp67trx). Its one record is that drop, and a key go-flags does not repeat
// is found by both. Its Go file is written into a copy of it (escapedTestdata).
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	vendored.RequireIdentical(test, sa5008.Analyzer, Analyzer, analysistest.TestData(), "differences")
	vendored.RequireOnlyAdjudicated(test, sa5008.Analyzer, Analyzer, escapedTestdata(test), "escaped")
}
