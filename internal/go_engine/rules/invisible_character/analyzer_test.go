package invisible_character

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/stylecheck/st1018"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamFolder is staticcheck's own fixtures for `ST1018` at Go 1.0, where the vendored copy keeps them, from this
// package's directory. They are read from there, never copied into this package's testdata: the fixture is not
// gofmt-clean, one comment is a space out of gofmt's alignment, and every Go file the module owns outside third_party
// must be, so a byte-for-byte copy can live only in the test's own temporary directory.
var upstreamFolder = filepath.Join("..", "..", "..", "..", "third_party", "honnef.co", "go", "tools", "stylecheck",
	"st1018", "testdata", "go1.0")

// upstreamTestdata is a testdata of the test's own, holding upstreamFolder copied byte for byte as go1.0, with the
// go.mod staticcheck's testutil lays over the folder, `module example.com` at Go 1.0, written to disk beside it since
// the engine's load reads go.mod from disk. parity's calls take one testdata, and this package's cannot hold the copy,
// so the copy is the port's own.
func upstreamTestdata(test *testing.T) string {
	test.Helper()
	testdata := test.TempDir()
	folder := filepath.Join(testdata, "go1.0")
	if copyError := os.CopyFS(folder, os.DirFS(upstreamFolder)); copyError != nil {
		test.Fatalf("copying %s: %v", upstreamFolder, copyError)
	}
	// testutil's own overlay, `module example.com\ngo 1.0`, with the newline gofmt and editors leave at the end.
	moduleFile := []byte("module example.com\ngo 1.0\n")
	if writeError := os.WriteFile(filepath.Join(folder, "go.mod"), moduleFile, 0o600); writeError != nil {
		test.Fatalf("writing the go.mod testutil lays over %s: %v", folder, writeError)
	}
	return testdata
}

// staticcheck's own fixtures for `ST1018` hold the port to every `//@ diag(...)` note the original expects, and to
// nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, upstreamTestdata(test), "go1.0")
}

// The port's fixes make the golden file staticcheck's fixtures hold beside CheckInvisibleCharacters.go, one section
// for each fix's message, as staticcheck's testutil requires of the original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, upstreamTestdata(test), "go1.0")
}

// Over staticcheck's fixtures the original and the port find the same findings, fixes and all. They sit in a testdata
// of their own, apart from parity's, so they take a call of their own.
func TestPortMatchesOriginalOverUpstreamFixtures(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1018.Analyzer, Analyzer, upstreamTestdata(test), "go1.0")
}

// Over parity's fixture, every character the check reads and every near miss, in every kind of literal it judges and
// the kind it leaves, the two find the same findings, fixes and all. The port differs from the original nowhere, so
// there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1018.Analyzer, Analyzer, analysistest.TestData(), "parity")
}
