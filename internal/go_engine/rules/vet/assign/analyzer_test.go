package assign

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/passes/assign"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for assign, copied unchanged into testdata/src with their golden files and run as x/tools'
// test runs them, with suggested fixes and no flags: every `// want` the original expects, the port reports at the
// same line with the same message, analysistest fails on a finding no want expects as much as on a want nothing
// found, and every fix the port offers, applied, turns each file into its golden one.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// Over x/tools' fixtures, parity's and statements', the last two holding every branch of the judgment and of the fix
// and the shapes most likely to differ, the original and the port find the same findings, each the same in every
// field, its suggested fixes edit for edit among them. statements is laid out by hand, which no .go file in the module
// may be, so it lives in testdata/statements.txtar and is extracted into a tree of its own, compared on its own. The
// port differs from the original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, assign.Analyzer, Analyzer, analysistest.TestData(), "a", "typeparams", "parity")
	parity.RequireIdentical(test, assign.Analyzer, Analyzer,
		extracted(test, filepath.Join(analysistest.TestData(), "statements.txtar")), "statements")
}

// extracted is the directory holding the files of the txtar archive at path, written out under their names, so a
// fixture laid out by hand reaches the loader byte for byte.
func extracted(test *testing.T, path string) string {
	test.Helper()
	archive, parseError := txtar.ParseFile(path)
	if parseError != nil {
		test.Fatalf("reading %s: %v", path, parseError)
	}
	if len(archive.Files) == 0 {
		test.Fatalf("%s holds no file, so the comparison would prove nothing", path)
	}
	directory := test.TempDir()
	for _, file := range archive.Files {
		filePath := filepath.Join(directory, filepath.FromSlash(file.Name))
		if directoryError := os.MkdirAll(filepath.Dir(filePath), 0o755); directoryError != nil {
			test.Fatalf("making the directory of %s: %v", filePath, directoryError)
		}
		if writeError := os.WriteFile(filePath, file.Data, 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", filePath, writeError)
		}
	}
	return directory
}
