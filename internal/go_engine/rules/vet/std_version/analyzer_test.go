package std_version

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/passes/stdversion"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPatterns are the packages of x/tools' test.txtar its test runs, in its order.
var upstreamPatterns = []string{
	"example.com/basic",
	"example.com/despite",
	"example.com/mod20",
	"example.com/mod21",
	"example.com/mod22",
	"example.com/old",
}

// parityPatterns are the module packages of parity.txtar, each module's go directive its own.
var parityPatterns = []string{
	"example.com/mod20",
	"example.com/mod21",
	"example.com/mod21patch",
	"example.com/mod24",
	"example.com/mod25",
}

// upstreamLoadErrors are the errors test.txtar's packages load with, each named by its path in the archive, since
// several modules hold a file of the same name. Only despite's syntax error is meant, to show both passes run despite
// errors. The rest are incidental to fixtures written to be read by the analyzer rather than compiled: a method value
// left unused, bytes never imported, and a type used as a value. Both passes run despite errors, so every finding on
// them is counted all the same.
var upstreamLoadErrors = []parity.LoadError{
	{
		Position: "basic/basic.go:14:2",
		Message:  "new(types.Info).PkgNameOf (value of type func(imp *ast.ImportSpec) *types.PkgName) is not used",
	},
	{Position: "despite/errors.go:13:1", Message: "expected declaration, found invalid"},
	{Position: "mod20/tag16.go:13:9", Message: "types.Alias (type) is not an expression"},
	{Position: "mod20/tag22.go:13:9", Message: "types.Alias (type) is not an expression"},
	{
		Position: "mod21/notag.go:14:2",
		Message:  "new(types.Info).PkgNameOf (value of type func(imp *ast.ImportSpec) *types.PkgName) is not used",
	},
	{Position: "mod21/tag16.go:15:9", Message: "types.Alias (type) is not an expression"},
	{Position: "mod21/tag22.go:14:9", Message: "types.Alias (type) is not an expression"},
	{Position: "mod22/notag.go:7:10", Message: "undefined: bytes"},
	{Position: "mod22/notag.go:8:10", Message: "types.Alias (type) is not an expression"},
	{Position: "mod22/tag16.go:15:9", Message: "types.Alias (type) is not an expression"},
	{Position: "old/tag21.go:9:9", Message: "types.Alias (type) is not an expression"},
}

// x/tools' own fixture for stdversion, test.txtar, copied unchanged into testdata and run as x/tools' test runs it,
// over the same six packages with no flags. Upstream extracts the archive with its internal testfiles, which this
// module cannot import, so it is extracted with the public txtar package instead, as testfiles does. Its go.work makes
// the load a workspace of modules, each with the go directive the fixture is about. The parity archive's wants, ours,
// run the same way, its modules in a workspace and its package outside one in a tree of its own, so what the parity
// test compares is what each line says. The harness holds the port to every want, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	test.Run("test.txtar", func(subtest *testing.T) {
		subtest.Parallel()
		directory := extractedArchive(subtest, "test.txtar")
		parity.RequireUpstreamWants(subtest, Analyzer, directory, upstreamPatterns...)
	})
	test.Run("parity.txtar's modules", func(subtest *testing.T) {
		subtest.Parallel()
		directory := extractedArchive(subtest, "parity.txtar")
		parity.RequireUpstreamWants(subtest, Analyzer, directory, parityPatterns...)
	})
	test.Run("parity.txtar outside a module", func(subtest *testing.T) {
		subtest.Parallel()
		directory := extractedArchive(subtest, "parity.txtar")
		parity.RequireUpstreamWants(subtest, Analyzer, filepath.Join(directory, "gopath"), "nomodule")
	})
}

// Over x/tools' fixture and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field, in each load: test.txtar's
// workspace, parity.txtar's workspace, and parity.txtar's package outside a module, each a subtest of its own.
// test.txtar's packages do not all type-check, so they load with exactly upstreamLoadErrors; parity's load clean.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	test.Run("test.txtar", func(subtest *testing.T) {
		subtest.Parallel()
		directory := extractedArchive(subtest, "test.txtar")
		parity.RequireIdenticalDespiteLoadErrors(subtest, stdversion.Analyzer, Analyzer, directory,
			upstreamLoadErrors, upstreamPatterns...)
	})
	test.Run("parity.txtar's modules", func(subtest *testing.T) {
		subtest.Parallel()
		directory := extractedArchive(subtest, "parity.txtar")
		parity.RequireIdentical(subtest, stdversion.Analyzer, Analyzer, directory, parityPatterns...)
	})
	test.Run("parity.txtar outside a module", func(subtest *testing.T) {
		subtest.Parallel()
		directory := extractedArchive(subtest, "parity.txtar")
		parity.RequireIdentical(subtest, stdversion.Analyzer, Analyzer, filepath.Join(directory, "gopath"), "nomodule")
	})
}

// extractedArchive is the directory the archive testdata/<name> is extracted into, as x/tools' internal
// testfiles.ExtractTxtarFileToTmp extracts one.
func extractedArchive(test *testing.T, name string) string {
	test.Helper()
	archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), name))
	if parseError != nil {
		test.Fatal(parseError)
	}
	files, filesError := txtar.FS(archive)
	if filesError != nil {
		test.Fatal(filesError)
	}
	directory := test.TempDir()
	if copyError := os.CopyFS(directory, files); copyError != nil {
		test.Fatal(copyError)
	}
	return directory
}
