package deprecated_use

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"honnef.co/go/tools/staticcheck/sa1019"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamFixtures is staticcheck's own testdata for SA1019, read where the vendored module keeps it.
var upstreamFixtures = filepath.Join("..", "..", "..", "..", "third_party", "honnef.co", "go", "tools", "staticcheck",
	"sa1019", "testdata")

// upstreamModules are the version folders of staticcheck's testdata for SA1019, each run as a module of that Go
// version, since the version decides which of the standard library's deprecations apply.
var upstreamModules = []string{"go1.0", "go1.3", "go1.4", "go1.8", "go1.18", "go1.19"}

// upstreamTree is a tree of the test's own holding staticcheck's testdata for SA1019 byte for byte as the vendored
// module keeps it, each version folder with the go.mod staticcheck's testutil lays over it, module example.com at that
// version, since the fixtures import each other by that path. One fixture is not gofmt-clean, and the repository's
// gofmt guard walks testdata, so the copy is made here rather than kept there, and the harness, which reads fixtures
// where a testdata holds them, is given this tree. go1.0's CheckDeprecated imports github.com/golang/protobuf/proto,
// which neither the release nor this repository carries: staticcheck's harness supplies a stand-in, since its wants
// read "Alas, it is deprecated.", not the real package's notice, and so does this test, testdata/protobuf, wired by a
// replace. Both sides read the same stand-in, so parity is untouched.
func upstreamTree(test *testing.T) string {
	test.Helper()
	tree := filepath.Join(test.TempDir(), "testdata")
	if copyError := os.CopyFS(tree, os.DirFS(upstreamFixtures)); copyError != nil {
		test.Fatalf("copying staticcheck's testdata for SA1019, which the vendored module must keep: %v", copyError)
	}
	protobuf, absoluteError := filepath.Abs(filepath.Join(analysistest.TestData(), "protobuf"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	for _, module := range upstreamModules {
		goMod := "module example.com\ngo " + strings.TrimPrefix(module, "go") + "\n"
		if module == "go1.0" {
			goMod += "require github.com/golang/protobuf v1.0.0\n" +
				"replace github.com/golang/protobuf => " + protobuf + "\n"
		}
		if writeError := os.WriteFile(filepath.Join(tree, module, "go.mod"), []byte(goMod), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return tree
}

// Every `//@ diag(...)` note the original expects in staticcheck's fixtures, the port reports on the same line with a
// message the note matches, and the port reports nothing no note expects, each version's fixture loaded as the module
// of its version.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, upstreamTree(test), upstreamModules...)
}

// Over staticcheck's fixtures and the house's parity modules, the original and the port find the same findings. The
// parity modules are one of Go 1.22, with a package of deprecated objects of every kind and a package using them every
// way the check reads a use, and one of Go 1.15, which uses the standard library's deprecations either side of that
// version.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1019.Analyzer, Analyzer, upstreamTree(test), upstreamModules...)
	parity.RequireIdentical(test, sa1019.Analyzer, Analyzer, analysistest.TestData(), "parity", "parity_older")
}

// The differences fixture holds the uses whose judgment hangs on the state the walk carries or on the shape of a
// selector, and records no difference, since there is none, so the test holds that the two agree there.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1019.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
