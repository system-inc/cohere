package assign

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/assign"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for assign, copied unchanged into testdata/src with their golden files and run as x/tools'
// test runs them, with suggested fixes and no flags: every `// want` the original expects, the port reports at the
// same line with the same message, analysistest fails on a finding no want expects as much as on a want nothing
// found, and every fix the port offers, applied, turns each file into its golden one.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, analysistest.TestData(), Analyzer, "a", "typeparams")
}

// The original and the port run in one checker over the same fixture packages, x/tools', parity's and statements',
// the last two holding every branch of the judgment and of the fix and the shapes most likely to differ, and their
// findings must be the same multiset of file, line, column, message and fix, edit for edit. statements is laid out by
// hand, which no .go file in the module may be, so it lives in testdata/statements.txtar and is extracted into a tree
// of its own. Each side's count is logged, since two empty sides would match and prove nothing, and the test fails if
// the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, analysistest.TestData(), "a", "typeparams", "parity")
	statementsOriginal, statementsPort := findingsOf(
		test, extracted(test, filepath.Join(analysistest.TestData(), "statements.txtar")), "statements",
	)
	test.Logf("in statements, the original found %d, the port %d", len(statementsOriginal), len(statementsPort))
	original, port = append(original, statementsOriginal...), append(port, statementsPort...)
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(port, compare)
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(original, port) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
	for _, extra := range difference(port, original) {
		test.Errorf("the port found %s, and the original did not", described(extra))
	}
}

// finding is one finding as the differential compares it.
type finding struct {
	file    string
	line    int
	column  int
	message string
	// fixes is the finding's suggested fixes, each its message and its edits as offsets into the file and the text
	// put there, so two findings with the same text but different edits differ.
	fixes string
}

// described is found as a finding line prints it, followed by its fixes.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d: %s %s", found.file, found.line, found.column, found.message, found.fixes)
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

// findingsOf loads the fixture packages patterns name from the `GOPATH` tree at directory, runs x/tools' assign and the
// port over them in one checker, and returns each one's findings.
func findingsOf(test *testing.T, directory string, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	// The checker passes over a root that does not type-check without a word, so a fixture with one type error would
	// compare nothing and read green. Every root must load clean, and there must be one.
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v found no package, so the comparison would prove nothing", patterns)
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %v", root.ID,
				root.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{assign.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			found := finding{
				file:    filepath.Base(position.Filename),
				line:    position.Line,
				column:  position.Column,
				message: diagnostic.Message,
				fixes:   fixesOf(action, diagnostic),
			}
			switch action.Analyzer {
			case assign.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	return original, port
}

// fixesOf is diagnostic's suggested fixes, each its message and every edit as `[start,end)"text"` with offsets into
// the edit's file, in the order the fix lists them, and a fix with no edits as its message alone.
func fixesOf(action *checker.Action, diagnostic analysis.Diagnostic) string {
	fixes := []string{}
	for _, fix := range diagnostic.SuggestedFixes {
		edits := []string{}
		for _, edit := range fix.TextEdits {
			file := action.Package.Fset.File(edit.Pos)
			edits = append(edits, fmt.Sprintf(
				"[%d,%d)%q", file.Offset(edit.Pos), file.Offset(edit.End), edit.NewText,
			))
		}
		fixes = append(fixes, fmt.Sprintf("{%s: %s}", fix.Message, strings.Join(edits, " ")))
	}
	return strings.Join(fixes, " ")
}

// difference is what of from is not in without, counting repeats, so a finding reported twice by one side and once by
// the other is a difference.
func difference(from []finding, without []finding) []finding {
	remaining := map[finding]int{}
	for _, found := range without {
		remaining[found]++
	}
	missing := []finding{}
	for _, found := range from {
		if remaining[found] > 0 {
			remaining[found]--
			continue
		}
		missing = append(missing, found)
	}
	return missing
}

// errorsOnly hands analysistest.RunWithSuggestedFixes a reporter that isn't a testing.TB. Given a testing.TB, Run
// first proves the go command works by building a throwaway program, and a runner of prebuilt test binaries refuses a
// test that builds. Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
