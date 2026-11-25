package deprecated_use

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"honnef.co/go/tools/staticcheck/sa1019"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
)

// upstreamModules are the version folders of staticcheck's testdata for SA1019, each run as a module of that Go
// version, since the version decides which of the standard library's deprecations apply.
var upstreamModules = []string{"go1.0", "go1.3", "go1.4", "go1.8", "go1.18", "go1.19"}

// upstreamTree is a tree of the test's own holding staticcheck's testdata for SA1019 byte for byte as the module
// cache keeps it, each version folder with the go.mod staticcheck's testutil lays over it, module example.com at that
// version, since the fixtures import each other by that path. One fixture is not gofmt-clean, and the repository's
// gofmt guard walks testdata, so the copy is made here rather than kept there, and the harness, which reads fixtures
// where a testdata holds them, is given this tree. go1.0's CheckDeprecated imports github.com/golang/protobuf/proto,
// which neither the release nor this repository carries: staticcheck's harness supplies a stand-in, since its wants
// read "Alas, it is deprecated.", not the real package's notice, and so does this test, testdata/protobuf, wired by a
// replace. Both sides read the same stand-in, so parity is untouched.
func upstreamTree(test *testing.T) string {
	test.Helper()
	upstreamFixtures := filepath.Join(parity.StaticcheckDirectory(test), "staticcheck", "sa1019", "testdata")
	tree := filepath.Join(test.TempDir(), "testdata")
	if copyError := os.CopyFS(tree, os.DirFS(upstreamFixtures)); copyError != nil {
		test.Fatalf("copying staticcheck's testdata for SA1019, which the module must keep: %v", copyError)
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

// The differences fixture holds the uses whose judgment hangs on the enclosing declaration or on the shape of a
// selector. The two agree on every one but a package-level variable after a deprecated function or method, whose use
// the port alone finds (#zvwfe26): after_deprecated.go's, and shapes.go's after a deprecated method and with a function
// literal for its initializer. shapes.go holds the shapes they agree on beside them: a use inside a deprecated method,
// a standard library deprecation, the package's own deprecated type, and a group with one spec deprecated. The fixture
// is a module, whose records the harness reads from its own folder and not from the package below it, so they are
// given here.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	const reason = "The original keeps the last function declaration it walked until the next one, so it judges a " +
		"package-level variable after a deprecated function or method as if inside it and misses the use; the port " +
		"judges each use by the declaration that holds it."
	const retired = "finds: example.com/differences/library.RetiredFunction is deprecated: use another."
	parity.RequireOnlyAdjudicated(test, sa1019.Analyzer, Analyzer, analysistest.TestData(), "differences",
		parity.Record{File: "differences/uses/after_deprecated.go", Line: 13, Text: retired, Reason: reason},
		parity.Record{File: "differences/uses/shapes.go", Line: 20, Text: retired, Reason: reason},
		parity.Record{File: "differences/uses/shapes.go", Line: 27, Text: retired, Reason: reason},
	)
}
