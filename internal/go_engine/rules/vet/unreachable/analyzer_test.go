package unreachable

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
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/analysis/passes/unreachable"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/checker/driverutil"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixture for unreachable, a.go and its golden file copied unchanged into testdata/src/a, run as x/tools'
// test runs it, with no flags and with its suggested fixes: every `// want` the original expects, the port reports at
// the same line with the same message, analysistest fails on a finding no want expects as much as on a want nothing
// found, so the port adds nothing there either, and every fix the port offers, applied, turns a.go into its golden
// file. The parity fixtures' wants are held to the port the same way, so each says truly what it holds.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, analysistest.TestData(), Analyzer, "a")
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, "parity", "parity/shadowed")
	analysistest.Run(errorsOnly{test}, extractedLayouts(test), Analyzer, "layout")
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the walk and the shapes most likely to differ, and the layouts' (testdata/layout.txtar),
// every way the statement deleted can share its lines. Their findings must be the same multiset of file, line,
// column, end, message and fix, edit for edit, each edit as its offsets and new text, which a golden file, gofmt'd
// before it is compared, would not hold to the character. Each side's count is logged, since two empty sides would
// match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, fixtureRoots(test))
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expectSame(test, "the port", original, port)
}

// adjudicatedPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and
// the message of a finding only the port makes, `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ, or `drops:` and the message of a finding only the original makes, which is
// wrong.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?)|drops: (.+?))\s*$`)

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, as found or as a renaming, every finding the
// original alone makes is recorded as dropped, with the reason it is wrong on the line above, or is the original's
// side of a renaming, and every record is matched. A finding the original makes and the port does not, unrecorded,
// fails, since the port is never worse than the original.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, loadedRoots(test, analysistest.TestData(), []string{"differences"}))
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expectedPort := []finding{}
	expectedOriginal := []finding{}
	paths, globError := filepath.Glob(filepath.Join(analysistest.TestData(), "src", "differences", "*.go"))
	if globError != nil {
		test.Fatal(globError)
	}
	for _, path := range paths {
		fileExpectedPort, fileExpectedOriginal := adjudicatedIn(test, path)
		expectedPort = append(expectedPort, fileExpectedPort...)
		expectedOriginal = append(expectedOriginal, fileExpectedOriginal...)
	}
	if len(expectedPort)+len(expectedOriginal) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	compareAdjudicated(test, "the port", adjudicatedOnly(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", adjudicatedOnly(difference(original, port)), expectedOriginal)
}

// The registry lists unreachable among the passes that read no type information, so a run may hand it a pass with
// none. The port runs here over every fixture package, the differences' too, with no types, package or type
// checker's information at all, and inspect's result built from the syntax alone, and must find what it finds with
// them, fix for fix, each fix held to what the checker's report holds it to.
func TestPortReadsNoTypeInformation(test *testing.T) {
	test.Parallel()
	roots := append(fixtureRoots(test), loadedRoots(test, analysistest.TestData(), []string{"differences"})...)
	_, typed := findingsOf(test, roots)
	untyped := []finding{}
	for _, loadedPackage := range roots {
		pass := &analysis.Pass{
			Analyzer: Analyzer,
			Fset:     loadedPackage.Fset,
			Files:    loadedPackage.Syntax,
			ResultOf: map[*analysis.Analyzer]any{inspect.Analyzer: inspector.New(loadedPackage.Syntax)},
			// The checker's report, which holds every fix to be well formed, and cuts an edit's end just past the
			// file's, as the deletion in a file cut off makes, back to the file's end.
			Report: func(diagnostic analysis.Diagnostic) {
				validateError := driverutil.ValidateFixes(loadedPackage.Fset, Analyzer, diagnostic.SuggestedFixes)
				if validateError != nil {
					test.Errorf("the port with no types offers a fix the checker refuses: %v", validateError)
				}
				untyped = append(untyped, findingAt(loadedPackage, diagnostic))
			},
		}
		if _, runError := Analyzer.Run(pass); runError != nil {
			test.Fatalf("running the port over %s with no types: %v", loadedPackage.ID, runError)
		}
	}
	slices.SortFunc(untyped, compareFindings)
	test.Logf("the port found %d with types, %d with none", len(typed), len(untyped))
	expectSame(test, "the port with no types", typed, untyped)
}

// finding is one finding as the differential compares it, its suggested fixes included.
type finding struct {
	file    string
	line    int
	column  int
	end     string
	message string
	fixes   string
}

// described is found as a finding line prints it, followed by its end and its fixes.
func described(found finding) string {
	return fmt.Sprintf(
		"%s:%d:%d (to %s): %s%s", found.file, found.line, found.column, found.end, found.message, found.fixes,
	)
}

// compareFindings orders findings by the line each prints.
func compareFindings(first finding, second finding) int {
	return cmp.Compare(described(first), described(second))
}

// findingAt is diagnostic, reported in loadedPackage, as the differential compares it.
func findingAt(loadedPackage *packages.Package, diagnostic analysis.Diagnostic) finding {
	position := loadedPackage.Fset.Position(diagnostic.Pos)
	end := loadedPackage.Fset.Position(diagnostic.End)
	return finding{
		file:    filepath.Base(position.Filename),
		line:    position.Line,
		column:  position.Column,
		end:     fmt.Sprintf("%d:%d", end.Line, end.Column),
		message: diagnostic.Message,
		fixes:   describedFixes(loadedPackage, diagnostic),
	}
}

// describedFixes is every suggested fix of diagnostic, in order, as its message and each edit's file, offsets and new
// text, in the order the fix lists them, so two findings match only where they offer the same edits to the character.
// A fix with no edits is its message alone.
func describedFixes(loadedPackage *packages.Package, diagnostic analysis.Diagnostic) string {
	var fixes strings.Builder
	for _, fix := range diagnostic.SuggestedFixes {
		fmt.Fprintf(&fixes, " [fix %q:", fix.Message)
		for _, edit := range fix.TextEdits {
			start := loadedPackage.Fset.Position(edit.Pos)
			end := loadedPackage.Fset.Position(edit.End)
			fmt.Fprintf(
				&fixes, " %s@%d-%d %q", filepath.Base(start.Filename), start.Offset, end.Offset, edit.NewText,
			)
		}
		fixes.WriteString("]")
	}
	return fixes.String()
}

// expectSame fails unless original and other, side's findings, are the same multiset, and the original found any.
func expectSame(test *testing.T, side string, original []finding, other []finding) {
	test.Helper()
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(original, other) {
		test.Errorf("the original found %s, and %s did not", described(missing), side)
	}
	for _, extra := range difference(other, original) {
		test.Errorf("%s found %s, and the original did not", side, described(extra))
	}
}

// illFormed is the fixture packages that do not type-check, by design: x/tools' a, whose functions mostly lack the
// return their results ask for, and truncated, which does not even parse. Every other fixture must load clean, or its
// findings would be the checker's guesses at broken code.
var illFormed = map[string]bool{"a": true, "truncated": true}

// fixtureRoots loads every fixture package the original and the port agree on: x/tools' and parity's from
// testdata/src, and the layouts' from testdata/layout.txtar, extracted.
func fixtureRoots(test *testing.T) []*packages.Package {
	test.Helper()
	roots := loadedRoots(test, analysistest.TestData(), []string{"a", "parity", "parity/shadowed"})
	return append(roots, loadedRoots(test, extractedLayouts(test), []string{"layout", "truncated"})...)
}

// loadedRoots loads the fixture packages patterns name from directory, a tree laid out by import path, as
// analysistest loads one, and fails unless each but the ill-formed loads clean.
func loadedRoots(test *testing.T, directory string, patterns []string) []*packages.Package {
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
		if len(root.Errors) > 0 && !illFormed[root.PkgPath] {
			test.Fatalf("fixture package %s does not load clean: %v", root.ID, root.Errors)
		}
	}
	return loaded.Roots
}

// extractedLayouts extracts testdata/layout.txtar into a temporary tree, a `GOPATH`, and returns its root.
func extractedLayouts(test *testing.T) string {
	test.Helper()
	archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), "layout.txtar"))
	if parseError != nil {
		test.Fatalf("reading layout.txtar: %v", parseError)
	}
	tree := test.TempDir()
	for _, file := range archive.Files {
		path := filepath.Join(tree, filepath.FromSlash(file.Name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, file.Data, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return tree
}

// findingsOf runs x/tools' unreachable and the port over roots in one checker, and returns each one's findings,
// sorted.
func findingsOf(test *testing.T, roots []*packages.Package) (original []finding, port []finding) {
	test.Helper()
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{unreachable.Analyzer, Analyzer}, roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %d packages: %v", len(roots), analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			found := findingAt(action.Package, diagnostic)
			switch action.Analyzer {
			case unreachable.Analyzer:
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

// adjudicatedIn reads the records of the differences fixture at path: the findings the port alone makes, and those
// the original alone makes, each as its file, line and message. A drop with no comment on the line above, its reason,
// fails.
func adjudicatedIn(test *testing.T, path string) (expectedPort []finding, expectedOriginal []finding) {
	test.Helper()
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	name := filepath.Base(path)
	previousLine := ""
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		for _, comment := range strings.Split(scanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", name, line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: name, line: line, message: match[1]})
			case match[4] != "":
				if !strings.HasPrefix(strings.TrimSpace(previousLine), "//") {
					test.Fatalf("%s:%d drops a finding with no reason on the line above", name, line)
				}
				expectedOriginal = append(expectedOriginal, finding{file: name, line: line, message: match[4]})
			default:
				expectedOriginal = append(expectedOriginal, finding{file: name, line: line, message: match[2]})
				expectedPort = append(expectedPort, finding{file: name, line: line, message: match[3]})
			}
		}
		previousLine = scanner.Text()
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	return expectedPort, expectedOriginal
}

// compareAdjudicated fails the test unless the findings side alone makes are exactly those the fixture records.
func compareAdjudicated(test *testing.T, side string, actual []finding, expected []finding) {
	test.Helper()
	for _, unrecorded := range difference(actual, expected) {
		test.Errorf("%s alone found %s, which the fixture does not record", side, described(unrecorded))
	}
	for _, unmatched := range difference(expected, actual) {
		test.Errorf("the fixture records that %s alone finds %s, and it does not", side, described(unmatched))
	}
}

// adjudicatedOnly is findings as the differences fixture's records name them, by file, line and message alone.
func adjudicatedOnly(findings []finding) []finding {
	named := []finding{}
	for _, found := range findings {
		named = append(named, finding{file: found.file, line: found.line, message: found.message})
	}
	return named
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

// errorsOnly hands analysistest a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
