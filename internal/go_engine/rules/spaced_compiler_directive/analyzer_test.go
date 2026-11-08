package spaced_compiler_directive

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
	"honnef.co/go/tools/staticcheck/sa9009"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// adjudicatedPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and
// the message of a finding only the port makes, or `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?))\s*$`)

// upstreamFixture is staticcheck's own fixture for SA9009, read where the vendored module keeps it. The check judges
// whether a comment starts in its line's first column, and the fixture's cases are comments indented before it, which
// gofmt would move there, so a gofmt-clean copy would test nothing; the repository's gofmt guard walks testdata, so an
// unchanged copy cannot live there. upstreamTree copies it byte for byte into a tree of the test's own instead.
var upstreamFixture = filepath.Join("..", "..", "..", "..", "third_party", "honnef.co", "go", "tools", "staticcheck",
	"sa9009", "testdata", "go1.0", "Directives", "pkg.go")

// upstreamPackage is the import path upstreamTree gives the fixture, its folder's name in staticcheck's testdata.
const upstreamPackage = "Directives"

// upstreamTree is a tree laid out by import path, made for the test, holding staticcheck's fixture for SA9009 byte for
// byte as the vendored module keeps it. The go1.0 folder sets the language version of staticcheck's run, which SA9009
// never reads.
func upstreamTree(test *testing.T) string {
	test.Helper()
	contents, readError := os.ReadFile(upstreamFixture)
	if readError != nil {
		test.Fatalf("reading staticcheck's fixture for SA9009, which the vendored module must keep: %v", readError)
	}
	// An empty fixture would load as no package at all, or one with nothing to find, and compare nothing.
	if len(contents) == 0 {
		test.Fatalf("staticcheck's fixture for SA9009 at %s is empty, so the comparison would prove nothing",
			upstreamFixture)
	}
	tree := test.TempDir()
	directory := filepath.Join(tree, "src", upstreamPackage)
	if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	copied := filepath.Join(directory, filepath.Base(upstreamFixture))
	if writeError := os.WriteFile(copied, contents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return tree
}

// Every `//@ diag(...)` note staticcheck's fixture expects, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	tree := upstreamTree(test)
	fixture := filepath.Join(tree, "src", upstreamPackage)
	wants, readError := staticcheck_wants.Read(fixture)
	if readError != nil {
		test.Fatal(readError)
	}
	if len(wants) == 0 {
		test.Fatal("the upstream fixture holds no notes, so the comparison proves nothing")
	}
	_, port := findingsOf(test, []string{tree}, upstreamPackage)
	found := []staticcheck_wants.Finding{}
	for _, each := range port {
		// The wants name a file by its full path, and the fixture's files all sit in its one folder.
		found = append(found, staticcheck_wants.Finding{
			File: filepath.Join(fixture, each.file), Line: each.line, Message: each.message,
		})
	}
	test.Logf("the fixture expects %d, the port found %d", len(wants), len(found))
	unexpected, untaken := staticcheck_wants.Compare(wants, found)
	for _, extra := range unexpected {
		test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
			extra.Message)
	}
	for _, missing := range untaken {
		test.Errorf("the port reported nothing matching %s", missing)
	}
}

// The original and the port run in one checker over the same fixture packages, staticcheck's and parity's, the second
// holding every way a comment can read as a directive or not that gofmt leaves standing, and a generated file, and
// their findings must be the same multiset of file, start, end and message. Each side's count is logged, since two
// empty sides would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	trees := []string{analysistest.TestData(), upstreamTree(test)}
	original, port := findingsOf(test, trees, upstreamPackage, "parity")
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

// Over the differences fixture the two may disagree only where each disagreement was adjudicated in the port's
// favour, recorded on its line as a `// port finds:` or `// port renames:` comment. The fixture holds the comments
// where the two could most plausibly part, a line directive moving the columns after it, a directive in a comment
// group's middle, quotes and backslashes the message quotes, and records none, since the port parts from the original
// nowhere: the test then requires the two to agree there finding for finding, and fails if the original finds
// nothing, so the agreement is over findings and not over silence.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, []string{analysistest.TestData()}, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the comparison proves nothing")
	}
	expectedPort := []finding{}
	expectedOriginal := []finding{}
	path := filepath.Join(analysistest.TestData(), "src", "differences", "differences.go")
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		for _, comment := range strings.Split(scanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("differences.go:%d holds a comment the test cannot read: %q", line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: "differences.go", line: line, message: match[1]})
			default:
				renamed := finding{file: "differences.go", line: line, message: match[2]}
				expectedOriginal = append(expectedOriginal, renamed)
				expectedPort = append(expectedPort, finding{file: "differences.go", line: line, message: match[3]})
			}
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	compareAdjudicated(test, "the port", positionsOnly(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", positionsOnly(difference(original, port)), expectedOriginal)
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

// finding is one finding as the differential compares it: its file by base name, where it starts and ends, and its
// message.
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

// findingsOf loads the fixture packages patterns name from trees, each laid out by import path, runs staticcheck's
// SA9009 and the port over them in one checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, trees []string, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	// The trees are one `GOPATH`, loaded as analysistest loads a tree, from the first of them.
	loaded, loadError := load.Packages(trees[0], patterns, []string{
		"GOPATH=" + strings.Join(trees, string(os.PathListSeparator)), "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		// A fixture that does not type-check is one both sides skip, and would compare nothing.
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{sa9009.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			end := action.Package.Fset.Position(diagnostic.End)
			found := finding{
				file:      filepath.Base(position.Filename),
				line:      position.Line,
				column:    position.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				message:   diagnostic.Message,
			}
			switch action.Analyzer {
			case sa9009.Analyzer:
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

// positionsOnly is findings with only their file, line and message kept, for comparing with the differences
// fixture's comments, which name a line.
func positionsOnly(findings []finding) []finding {
	kept := []finding{}
	for _, found := range findings {
		kept = append(kept, finding{file: found.file, line: found.line, message: found.message})
	}
	return kept
}
