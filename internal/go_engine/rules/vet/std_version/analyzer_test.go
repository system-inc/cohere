package std_version

import (
	"cmp"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/stdversion"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
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

// x/tools' own fixture for stdversion, test.txtar, copied unchanged into testdata and run as x/tools' test runs it,
// over the same six packages with no flags. Upstream extracts the archive with its internal testfiles, which this
// module cannot import, so it is extracted with the public txtar package instead, as testfiles does. Its go.work makes
// the load a workspace of modules, each with the go directive the fixture is about. The parity archive's wants, ours,
// run the same way, its modules in a workspace and its package outside one in a tree of its own, so what the parity
// test compares is what each line says. Every `// want` is met, and analysistest fails on a finding no want expects as
// much as on a want nothing found.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	test.Run("test.txtar", func(subtest *testing.T) {
		subtest.Parallel()
		directory := extractedArchive(subtest, "test.txtar")
		analysistest.Run(errorsOnly{subtest}, directory, Analyzer, upstreamPatterns...)
	})
	test.Run("parity.txtar's modules", func(subtest *testing.T) {
		subtest.Parallel()
		directory := extractedArchive(subtest, "parity.txtar")
		analysistest.Run(errorsOnly{subtest}, directory, Analyzer, parityPatterns...)
	})
	test.Run("parity.txtar outside a module", func(subtest *testing.T) {
		subtest.Parallel()
		directory := extractedArchive(subtest, "parity.txtar")
		analysistest.Run(errorsOnly{subtest}, filepath.Join(directory, "gopath"), Analyzer, "nomodule")
	})
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment and the shapes most likely to differ, and their findings must be the same
// multiset of file, line, column, end and message. Each side's count is logged per load, since two empty sides would
// match and prove nothing, and the test fails if the original finds nothing in a load.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	upstream := extractedArchive(test, "test.txtar")
	parity := extractedArchive(test, "parity.txtar")
	loads := []fixtureLoad{
		{
			name:        "test.txtar",
			directory:   upstream,
			environment: []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=" + filepath.Join(upstream, "go.work")},
			patterns:    upstreamPatterns,
			// x/tools' fixtures hold type errors, despite's on purpose and others by the way (a method value left
			// unused, bytes never imported, a type used as a value), and both passes run despite errors, so every
			// finding on them is counted all the same.
			typeErrorsAllowed: true,
		},
		{
			name:        "parity.txtar's modules",
			directory:   parity,
			environment: []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=" + filepath.Join(parity, "go.work")},
			patterns:    parityPatterns,
		},
		{
			name:      "parity.txtar outside a module",
			directory: filepath.Join(parity, "gopath"),
			environment: []string{
				"GOPATH=" + filepath.Join(parity, "gopath"), "GO111MODULE=off", "GOWORK=off",
			},
			patterns: []string{"nomodule"},
		},
	}
	for _, fixtures := range loads {
		original, port := findingsOf(test, fixtures)
		test.Logf("%s: the original found %d, the port %d", fixtures.name, len(original), len(port))
		if len(original) == 0 {
			test.Errorf("%s: the original found nothing, so the comparison proves nothing", fixtures.name)
		}
		for _, missing := range difference(original, port) {
			test.Errorf("%s: the original found %s, and the port did not", fixtures.name, described(missing))
		}
		for _, extra := range difference(port, original) {
			test.Errorf("%s: the port found %s, and the original did not", fixtures.name, described(extra))
		}
	}
}

// fixtureLoad is one load of fixture packages: from directory, under environment, the packages patterns name, which
// must type-check unless typeErrorsAllowed.
type fixtureLoad struct {
	name              string
	directory         string
	environment       []string
	patterns          []string
	typeErrorsAllowed bool
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

// finding is one finding as the differential compares it.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
}

// described is found as a finding line prints it, with where it ends.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
}

// findingOf is diagnostic as the differential compares it, its file named from the load's directory, since several
// fixture packages hold a file of the same name.
func findingOf(fileSet *token.FileSet, directory string, diagnostic analysis.Diagnostic) finding {
	position := fileSet.Position(diagnostic.Pos)
	end := fileSet.Position(diagnostic.End)
	file, relativeError := filepath.Rel(directory, position.Filename)
	if relativeError != nil {
		file = position.Filename
	}
	return finding{
		file:      filepath.ToSlash(file),
		line:      position.Line,
		column:    position.Column,
		endLine:   end.Line,
		endColumn: end.Column,
		message:   diagnostic.Message,
	}
}

// findingsOf loads the fixture packages fixtures names, runs x/tools' stdversion and the port over them in one
// checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, fixtures fixtureLoad) (original []finding, port []finding) {
	test.Helper()
	loaded, loadError := load.Packages(fixtures.directory, fixtures.patterns, fixtures.environment, nil)
	if loadError != nil {
		test.Fatalf("%s: loading %v: %v", fixtures.name, fixtures.patterns, loadError)
	}
	if len(loaded.Roots) != len(fixtures.patterns) {
		test.Fatalf("%s: loading %v found %d packages, not one for each", fixtures.name, fixtures.patterns,
			len(loaded.Roots))
	}
	// Both passes run despite errors, so a root that does not type-check is still compared, but a parity fixture is
	// written to type-check, and an error there would mean it says less than it seems to.
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 && !fixtures.typeErrorsAllowed {
			test.Fatalf("%s: fixture package %s does not type-check: %v", fixtures.name, root.ID, root.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{stdversion.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("%s: analyzing %v: %v", fixtures.name, fixtures.patterns, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("%s: running %s: %v", fixtures.name, action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			found := findingOf(action.Package.Fset, fixtures.directory, diagnostic)
			switch action.Analyzer {
			case stdversion.Analyzer:
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
