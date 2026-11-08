package deprecated_use

import (
	"bufio"
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/staticcheck/sa1019"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// moduleEnvironment loads a fixture module as analysistest does, in module mode with nothing fetched.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// upstreamFixtures is staticcheck's own testdata for SA1019, read where the vendored module keeps it.
var upstreamFixtures = filepath.Join("..", "..", "..", "..", "third_party", "honnef.co", "go", "tools", "staticcheck",
	"sa1019", "testdata")

// upstreamModules are the version folders of staticcheck's testdata for SA1019, each run as a module of that Go
// version, since the version decides which of the standard library's deprecations apply.
var upstreamModules = []string{"go1.0", "go1.3", "go1.4", "go1.8", "go1.18", "go1.19"}

// upstreamTree is a tree of the test's own holding staticcheck's testdata for SA1019 byte for byte as the vendored
// module keeps it, each version folder with the go.mod staticcheck's testutil lays over it, module example.com at that
// version, since the fixtures import each other by that path. One fixture is not gofmt-clean, and the repository's
// gofmt guard walks testdata, so the copy is made here rather than kept there. go1.0's CheckDeprecated imports
// github.com/golang/protobuf/proto, which neither the release nor this repository carries: staticcheck's harness
// supplies a stand-in, since its wants read "Alas, it is deprecated.", not the real package's notice, and so does
// this test, testdata/protobuf, wired by a replace. Both sides read the same stand-in, so parity is untouched.
func upstreamTree(test *testing.T) string {
	test.Helper()
	tree := test.TempDir()
	copied := 0
	walkError := filepath.WalkDir(upstreamFixtures, func(path string, entry fs.DirEntry, entryError error) error {
		if entryError != nil {
			return entryError
		}
		relative, relativeError := filepath.Rel(upstreamFixtures, path)
		if relativeError != nil {
			return relativeError
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(tree, relative), 0o755)
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		copied++
		return os.WriteFile(filepath.Join(tree, relative), contents, 0o644)
	})
	if walkError != nil {
		test.Fatalf("copying staticcheck's testdata for SA1019, which the vendored module must keep: %v", walkError)
	}
	// A tree that lost its files would load nothing and compare nothing.
	if copied == 0 {
		test.Fatalf("staticcheck's testdata for SA1019 at %s holds no files", upstreamFixtures)
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
// message the note matches, and the port reports nothing no note expects, as staticcheck_wants matches them, each
// version's fixture loaded as the module of its version.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	tree := upstreamTree(test)
	wanted := 0
	for _, module := range upstreamModules {
		directory := filepath.Join(tree, module)
		wants, readError := staticcheck_wants.Read(directory)
		if readError != nil {
			test.Fatal(readError)
		}
		wanted += len(wants)
		graph := analyze(test, []*analysis.Analyzer{Analyzer}, directory)
		findings := []staticcheck_wants.Finding{}
		for _, action := range graph.Roots {
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			for _, diagnostic := range action.Diagnostics {
				position := action.Package.Fset.Position(diagnostic.Pos)
				findings = append(findings, staticcheck_wants.Finding{
					File:    position.Filename,
					Line:    position.Line,
					Message: diagnostic.Message,
				})
			}
		}
		test.Logf("%s: the fixture wants %d, the port reported %d", module, len(wants), len(findings))
		unexpected, untaken := staticcheck_wants.Compare(wants, findings)
		for _, found := range unexpected {
			test.Errorf("%s:%d: the port reported %q, which no note on the line expects", found.File, found.Line,
				found.Message)
		}
		for _, want := range untaken {
			test.Errorf("the port reported nothing matching %s", want)
		}
	}
	if wanted == 0 {
		test.Fatal("the upstream fixtures hold no notes, so the comparison proves nothing")
	}
}

// parityModules are the house's parity modules: one of Go 1.22, with a package of deprecated objects of every kind and
// a package using them every way the check reads a use, and one of Go 1.15, which uses the standard library's
// deprecations either side of that version.
var parityModules = []string{"parity", "parity_older"}

// The original and the port run in one checker over the same fixture modules, staticcheck's and the house's parity
// modules, and their findings must be the same multiset of file, start, end and message. Each side's count is logged,
// since two empty sides would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	tree := upstreamTree(test)
	original := []finding{}
	port := []finding{}
	modules := []string{}
	for _, module := range upstreamModules {
		modules = append(modules, filepath.Join(tree, module))
	}
	for _, module := range parityModules {
		modules = append(modules, filepath.Join(analysistest.TestData(), module))
	}
	for _, module := range modules {
		moduleOriginal, modulePort := findingsOf(test, module)
		test.Logf("%s: the original found %d, the port %d", filepath.Base(module), len(moduleOriginal),
			len(modulePort))
		original = append(original, moduleOriginal...)
		port = append(port, modulePort...)
	}
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

// adjudicatedPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and
// the message of a finding only the port makes, or `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?))\s*$`)

// Over the differences fixture the two may disagree only where each disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, as found or as a renaming, every finding the
// original alone makes is the original's side of a recorded renaming, and every record is matched. A finding the
// original makes and the port does not, unrecorded, fails, since the port is never worse than the original. The
// fixture holds the uses whose judgment hangs on the state the walk carries or on the shape of a selector, and records
// no difference, since there is none, so the test holds that the two agree there, and fails should the original find
// nothing in it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, filepath.Join(analysistest.TestData(), "differences"))
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the agreement proves nothing")
	}
	expectedPort, expectedOriginal := recorded(test)
	test.Logf("the fixture records %d differences", len(expectedPort))
	compareAdjudicated(test, "the port", withoutColumns(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", withoutColumns(difference(original, port)), expectedOriginal)
}

// differencesFile is the file of the differences module the records are read from, named as findings name it.
var differencesFile = filepath.Join("differences", "uses", "uses.go")

// recorded is what the comments of the differences module's uses file record: the findings the port alone makes, and
// the original's side of each renaming.
func recorded(test *testing.T) (expectedPort []finding, expectedOriginal []finding) {
	test.Helper()
	expectedPort = []finding{}
	expectedOriginal = []finding{}
	file, openError := os.Open(filepath.Join(analysistest.TestData(), differencesFile))
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	fileScanner := bufio.NewScanner(file)
	for line := 1; fileScanner.Scan(); line++ {
		for _, comment := range strings.Split(fileScanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", differencesFile, line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: differencesFile, line: line, message: match[1]})
			default:
				renamed := finding{file: differencesFile, line: line, message: match[2]}
				expectedOriginal = append(expectedOriginal, renamed)
				expectedPort = append(expectedPort, finding{file: differencesFile, line: line, message: match[3]})
			}
		}
	}
	if scanError := fileScanner.Err(); scanError != nil {
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

// finding is one finding as the differential compares it, its file named from its module's folder, since several
// modules hold files of the same name, and its range from start to end.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
}

// described is found as a finding line prints it, with its end.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
}

// findingOf is diagnostic, reported by action in the module at directory, as a finding named from the module's
// folder.
func findingOf(test *testing.T, directory string, action *checker.Action, diagnostic analysis.Diagnostic) finding {
	test.Helper()
	position := action.Package.Fset.Position(diagnostic.Pos)
	end := action.Package.Fset.Position(diagnostic.End)
	relative, relativeError := filepath.Rel(filepath.Dir(directory), position.Filename)
	if relativeError != nil {
		test.Fatal(relativeError)
	}
	return finding{
		file:      relative,
		line:      position.Line,
		column:    position.Column,
		endLine:   end.Line,
		endColumn: end.Column,
		message:   diagnostic.Message,
	}
}

// analyze loads every package of the fixture module at directory, its tests included, and runs analyzers over them
// in one checker. A root package that does not type-check fails the test, since the checker skips it and would
// compare nothing.
func analyze(test *testing.T, analyzers []*analysis.Analyzer, directory string) *checker.Graph {
	test.Helper()
	loaded, loadError := load.Packages(directory, []string{"./..."}, moduleEnvironment, nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %s found no package", directory)
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	// The packages, not the roots: a package with tests is analyzed once, as its test variant, which holds all its
	// files, as staticcheck's runner analyzes it, where the roots hold both and would report its files twice.
	graph, analyzeError := checker.Analyze(analyzers, loaded.Packages, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", directory, analyzeError)
	}
	return graph
}

// findingsOf runs staticcheck's SA1019 and the port over the fixture module at directory in one checker, and returns
// each one's findings, sorted.
func findingsOf(test *testing.T, directory string) (original []finding, port []finding) {
	test.Helper()
	graph := analyze(test, []*analysis.Analyzer{sa1019.Analyzer, Analyzer}, directory)
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			found := findingOf(test, directory, action, diagnostic)
			switch action.Analyzer {
			case sa1019.Analyzer:
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

// withoutColumns is findings with their columns and ends dropped, for comparing with the differences fixture's
// comments, which name a line.
func withoutColumns(findings []finding) []finding {
	dropped := []finding{}
	for _, found := range findings {
		dropped = append(dropped, finding{file: found.file, line: found.line, message: found.message})
	}
	return dropped
}
