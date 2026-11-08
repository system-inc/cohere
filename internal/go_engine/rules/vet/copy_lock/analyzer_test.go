package copy_lock

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/copylock"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for copylock, copied unchanged into testdata/src and run as x/tools' Test runs them, with no
// flags: every `// want` the original expects, the port reports at the same line with the same message, and
// analysistest fails on a finding no want expects as much as on a want nothing found, so the port adds nothing there
// either.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test}, analysistest.TestData(), Analyzer, "a", "typeparams", "issue67787", "unfortunate",
	)
}

// x/tools' TestVersions22: a module at Go 1.22, whose files without a //go:build line declare their loop variables
// afresh on every iteration. x/tools extracts the archive with its internal testfiles, which the port's test cannot
// import, so extracted does the same with the public txtar package.
func TestPortMatchesUpstreamWantsAtGo122(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, extracted(test, "forstmt/go22.txtar"), Analyzer, "golang.org/fake/forstmt")
}

// x/tools' TestVersions21: the same files in a module at Go 1.21, where only the one whose //go:build line names Go
// 1.22 declares its loop variables afresh.
func TestPortMatchesUpstreamWantsAtGo121(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, extracted(test, "forstmt/go21.txtar"), Analyzer, "golang.org/fake/forstmt")
}

// fixture is one load the differential runs both analyzers over: a `GOPATH` tree or an extracted module, and the
// packages named in it.
type fixture struct {
	name      string
	directory string
	patterns  []string
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment and the shapes most likely to differ, and their findings must be the same
// multiset of file, line, column and message. Each load is compared on its own, since the archives' files share base
// names, and each side's count is logged, since two empty sides would match and prove nothing, and the test fails
// if the original finds nothing in a load.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	fixtures := []fixture{
		{
			name:      "testdata",
			directory: analysistest.TestData(),
			patterns:  []string{"a", "typeparams", "issue67787", "unfortunate", "parity"},
		},
		{
			name:      "forstmt/go22.txtar",
			directory: extracted(test, "forstmt/go22.txtar"),
			patterns:  []string{"golang.org/fake/forstmt"},
		},
		{
			name:      "forstmt/go21.txtar",
			directory: extracted(test, "forstmt/go21.txtar"),
			patterns:  []string{"golang.org/fake/forstmt"},
		},
		{
			name:      "parity/patch_release.txtar",
			directory: extracted(test, "parity/patch_release.txtar"),
			patterns:  []string{"golang.org/fake/versions"},
		},
		{
			name:      "parity/prerelease.txtar",
			directory: extracted(test, "parity/prerelease.txtar"),
			patterns:  []string{"golang.org/fake/versions"},
		},
	}
	originalTotal := 0
	portTotal := 0
	for _, loaded := range fixtures {
		original, port := findingsOf(test, loaded.directory, loaded.patterns...)
		test.Logf("%s: the original found %d, the port %d", loaded.name, len(original), len(port))
		originalTotal += len(original)
		portTotal += len(port)
		if len(original) == 0 {
			test.Errorf("the original found nothing in %s, so the comparison proves nothing there", loaded.name)
		}
		for _, missing := range difference(original, port) {
			test.Errorf("in %s the original found %s, and the port did not", loaded.name, described(missing))
		}
		for _, extra := range difference(port, original) {
			test.Errorf("in %s the port found %s, and the original did not", loaded.name, described(extra))
		}
	}
	test.Logf("in all, the original found %d, the port %d", originalTotal, portTotal)
}

// Over the differences fixture, code that does not type-check, the original panics (see the fixture's package doc),
// so it cannot run beside the port, and the port runs alone. Its wants are the findings the port makes where a value
// has a name, and analysistest fails on any other finding, so the port reports nothing for a value with none.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, "differences")
}

// finding is one finding as the differential compares it.
type finding struct {
	file    string
	line    int
	column  int
	message string
}

// described is found as a finding line prints it.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d: %s", found.file, found.line, found.column, found.message)
}

// extracted is the txtar archive at name under testdata/src, written into a directory of the test's own, which the
// test removes when it ends. x/tools' testfiles.ExtractTxtarFileToTmp.
func extracted(test *testing.T, name string) string {
	test.Helper()
	archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), "src", name))
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

// findingsOf loads the fixture packages patterns name in directory, runs x/tools' copylock and the port over them in
// one checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, directory string, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	// A directory holding go.mod is a module's root, and any other a tree laid out by import path, each loaded as
	// analysistest loads it.
	environment := []string{"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off"}
	if _, statError := os.Stat(filepath.Join(directory, "go.mod")); statError == nil {
		environment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}
	}
	loaded, loadError := load.Packages(directory, patterns, environment, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{copylock.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			}
			switch action.Analyzer {
			case copylock.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(port, compare)
	return original, port
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

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
