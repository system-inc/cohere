package build_tag

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildtag"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for buildtag, byte for byte in testdata/upstream.txtar, run as x/tools' test runs them, over
// packages a and b with no flags: every `// want` the original expects, the port reports at the same line with the
// same message, and analysistest fails on a finding no want expects as much as on a want nothing found, so the port
// adds nothing there either. Most of the fixtures are files the build excludes, and three are assembly:
// analysistest reads the wants of a package's other files, and, for an analyzer named buildtag, of its excluded files
// too, as x/tools' does.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, extractedFixtures(test), Analyzer, "a", "b")
}

// parityPatterns are the packages both sides run over for parity: x/tools' fixtures and parity's, the second holding
// every branch of the judgment, the shapes most likely to differ, and clean cases beside them.
var parityPatterns = []string{"a", "b", "parity"}

// The original and the port run in one checker over the same fixture packages, and their findings must be the same
// multiset of file, line, column, end and message. Neither offers a fix. Each side's count is logged, since two empty
// sides would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, loadedRoots(test, parityPatterns))
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expectSame(test, original, port)
}

// The registry lists buildtag among the passes that read no type information, so a run may hand it a pass with
// none. The port runs here over the same packages with no types, package or type checker's information at all, and
// must find what the original finds with them.
func TestPortReadsNoTypeInformation(test *testing.T) {
	test.Parallel()
	roots := loadedRoots(test, parityPatterns)
	original, _ := findingsOf(test, roots)
	untyped := []finding{}
	for _, loadedPackage := range roots {
		pass := &analysis.Pass{
			Analyzer:     Analyzer,
			Fset:         loadedPackage.Fset,
			Files:        loadedPackage.Syntax,
			OtherFiles:   loadedPackage.OtherFiles,
			IgnoredFiles: loadedPackage.IgnoredFiles,
			ReadFile:     os.ReadFile,
			ResultOf:     map[*analysis.Analyzer]any{},
			Report: func(diagnostic analysis.Diagnostic) {
				untyped = append(untyped, findingAt(loadedPackage, diagnostic))
			},
		}
		if _, runError := Analyzer.Run(pass); runError != nil {
			test.Fatalf("running the port over %s with no types: %v", loadedPackage.ID, runError)
		}
	}
	slices.SortFunc(untyped, compareFindings)
	test.Logf("the original found %d, the port with no types %d", len(original), len(untyped))
	expectSame(test, original, untyped)
}

// recordPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and the
// message of a finding only the port makes. The port renames and drops nothing.
var recordPattern = regexp.MustCompile(`^finds: (.+?)\s*$`)

// Over the differences fixture the two disagree, and only where the disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, the original makes none the port does not,
// and every record is matched.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	directory := extractedFixtures(test)
	original, port := findingsOf(test, loadedRootsFrom(test, directory, []string{"differences"}))
	test.Logf("the original found %d, the port %d", len(original), len(port))
	recorded := []finding{}
	entries, readError := os.ReadDir(filepath.Join(directory, "src", "differences"))
	if readError != nil {
		test.Fatal(readError)
	}
	for _, entry := range entries {
		recorded = append(recorded, recordsIn(test, filepath.Join(directory, "src", "differences", entry.Name()))...)
	}
	if len(recorded) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	for _, unrecorded := range difference(lineOnly(difference(port, original)), recorded) {
		test.Errorf("the port alone found %s, which the fixture does not record", described(unrecorded))
	}
	for _, unmatched := range difference(recorded, lineOnly(difference(port, original))) {
		test.Errorf("the fixture records that the port alone finds %s, and it does not", described(unmatched))
	}
	for _, missing := range difference(original, port) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
}

// recordsIn is every finding the fixture file at path records as the port's alone, each at its line.
func recordsIn(test *testing.T, path string) []finding {
	test.Helper()
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	recorded := []finding{}
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		for _, comment := range strings.Split(scanner.Text(), "// port ")[1:] {
			match := recordPattern.FindStringSubmatch(comment)
			if match == nil {
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", filepath.Base(path), line, comment)
			}
			recorded = append(recorded, finding{file: filepath.Base(path), line: line, message: match[1]})
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	return recorded
}

// lineOnly is findings with their columns and ends dropped, for comparing with the differences fixture's records,
// which name a line.
func lineOnly(findings []finding) []finding {
	dropped := []finding{}
	for _, found := range findings {
		found.column = 0
		found.end = ""
		dropped = append(dropped, found)
	}
	return dropped
}

// finding is one finding as the differential compares it.
type finding struct {
	file    string
	line    int
	column  int
	end     string
	message string
}

// described is found as a finding line prints it.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%s: %s", found.file, found.line, found.column, found.end, found.message)
}

// compareFindings orders findings by the line each prints.
func compareFindings(first finding, second finding) int {
	return cmp.Compare(described(first), described(second))
}

// findingAt is diagnostic, reported in loadedPackage, as the differential compares it. The end is its line and
// column, and empty when the finding has none, as neither side's findings do: both report a position alone.
func findingAt(loadedPackage *packages.Package, diagnostic analysis.Diagnostic) finding {
	position := loadedPackage.Fset.Position(diagnostic.Pos)
	end := ""
	if diagnostic.End.IsValid() {
		endPosition := loadedPackage.Fset.Position(diagnostic.End)
		end = fmt.Sprintf("%d:%d", endPosition.Line, endPosition.Column)
	}
	return finding{
		file:    filepath.Base(position.Filename),
		line:    position.Line,
		column:  position.Column,
		end:     end,
		message: diagnostic.Message,
	}
}

// expectSame fails unless original and other are the same multiset of findings, and the original found any.
func expectSame(test *testing.T, original []finding, other []finding) {
	test.Helper()
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(original, other) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
	for _, extra := range difference(other, original) {
		test.Errorf("the port found %s, and the original did not", described(extra))
	}
}

// fixtureArchives are the fixture trees, each laid out by import path: x/tools' packages a and b, parity's package,
// and the differences package.
var fixtureArchives = []string{"upstream.txtar", "parity.txtar", "differences.txtar"}

// writtenFixtures are parity's files an archive cannot hold, written by the test: lines that end in a carriage
// return and a newline, which a checkout could normalize, and a last line with no newline, since an archive ends
// every file it holds with one. In the Go file the scanner drops the carriage returns from a block comment's text,
// so the positions of the lines read inside it are counted over the shorter text, the same on both sides.
var writtenFixtures = map[string]string{
	"carriage_return.s": "//go:build !never\r\n// +build never\r\n\r\n#include \"textflag.h\"\r\n",
	"carriage_return.go": "//go:build !never\r\n\r\n/*\r\n// +build late\r\n//go:build late\r\n*/\r\n\r\n" +
		"package parity\r\n",
	"no_newline.s": "//go:build !never\n// +build never",
}

// extractedFixtures extracts every fixture archive into one temporary tree, a `GOPATH`, writes the fixtures no
// archive holds, and returns its root.
func extractedFixtures(test *testing.T) string {
	test.Helper()
	tree := test.TempDir()
	for _, archiveName := range fixtureArchives {
		archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), archiveName))
		if parseError != nil {
			test.Fatalf("reading %s: %v", archiveName, parseError)
		}
		for _, file := range archive.Files {
			writeFixture(test, filepath.Join(tree, filepath.FromSlash(file.Name)), file.Data)
		}
	}
	for name, content := range writtenFixtures {
		writeFixture(test, filepath.Join(tree, "src", "parity", name), []byte(content))
	}
	return tree
}

// writeFixture writes content at path, making its directory first.
func writeFixture(test *testing.T, path string, content []byte) {
	test.Helper()
	if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(path, content, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}

// loadedRoots loads the fixture packages patterns name from a freshly extracted tree.
func loadedRoots(test *testing.T, patterns []string) []*packages.Package {
	test.Helper()
	return loadedRootsFrom(test, extractedFixtures(test), patterns)
}

// loadedRootsFrom loads the fixture packages patterns name from directory, each with its test variants, as
// analysistest loads one. Every root must load clean, and there must be one: the checker passes over a root that
// does not load without a word, so a fixture the go command refuses would compare nothing and read green.
func loadedRootsFrom(test *testing.T, directory string, patterns []string) []*packages.Package {
	test.Helper()
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v found no package, so the comparison would prove nothing", patterns)
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %v", root.ID,
				root.Errors)
		}
	}
	return loaded.Roots
}

// findingsOf runs x/tools' buildtag and the port over roots in one checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, roots []*packages.Package) (original []finding, port []finding) {
	test.Helper()
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{buildtag.Analyzer, Analyzer}, roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %d packages: %v", len(roots), analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			if len(diagnostic.SuggestedFixes) > 0 {
				test.Errorf("%s offers a fix, which neither side is known to: %s", action, diagnostic.Message)
			}
			found := findingAt(action.Package, diagnostic)
			switch action.Analyzer {
			case buildtag.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	slices.SortFunc(original, compareFindings)
	slices.SortFunc(port, compareFindings)
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
