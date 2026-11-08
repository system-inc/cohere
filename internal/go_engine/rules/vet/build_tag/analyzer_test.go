package build_tag

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildtag"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for buildtag, byte for byte in testdata/upstream.txtar, over packages a and b with no flags,
// hold the port to every want the original expects, and to nothing more. Most of the fixtures are files the build
// excludes, and three are assembly: analysistest reads the wants of a package's other files, and, for an analyzer
// named buildtag, of its excluded files too, as x/tools' does.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, extractedFixtures(test), "a", "b")
}

// parityPatterns are the packages both sides run over for parity: x/tools' fixtures and parity's, the second holding
// every branch of the judgment, the shapes most likely to differ, and clean cases beside them.
var parityPatterns = []string{"a", "b", "parity"}

// Over the fixture packages, extracted into a tree laid out by import path, the original and the port find the same
// findings, each the same in every field.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, buildtag.Analyzer, Analyzer, extractedFixtures(test), parityPatterns...)
}

// The registry lists buildtag among the passes that read no type information, so a run may hand it a pass with
// none. The port runs here over the same packages with no types, package or type checker's information at all, and
// must find what the original finds with them. The harness's checker always hands a pass its types, so the port runs
// on passes built by hand, and the original's findings, with types, come from the harness's Findings over the same
// tree, each compared whole by its Difference.
func TestPortReadsNoTypeInformation(test *testing.T) {
	test.Parallel()
	directory := extractedFixtures(test)
	original, _ := parity.Findings(test, buildtag.Analyzer, Analyzer, directory, parityPatterns...)
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	untyped := []parity.Finding{}
	for _, loadedPackage := range loadedRoots(test, directory, parityPatterns) {
		pass := &analysis.Pass{
			Analyzer:     Analyzer,
			Fset:         loadedPackage.Fset,
			Files:        loadedPackage.Syntax,
			OtherFiles:   loadedPackage.OtherFiles,
			IgnoredFiles: loadedPackage.IgnoredFiles,
			ReadFile:     os.ReadFile,
			ResultOf:     map[*analysis.Analyzer]any{},
			Report: func(diagnostic analysis.Diagnostic) {
				untyped = append(untyped, findingAt(test, directory, loadedPackage, diagnostic))
			},
		}
		if _, runError := Analyzer.Run(pass); runError != nil {
			test.Fatalf("running the port over %s with no types: %v", loadedPackage.ID, runError)
		}
	}
	test.Logf("the original found %d, the port with no types %d", len(original), len(untyped))
	for _, missing := range parity.Difference(original, untyped) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
	for _, extra := range parity.Difference(untyped, original) {
		test.Errorf("the port found %s, and the original did not", extra)
	}
}

// recordPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and the
// message of a finding only the port makes. The port renames and drops nothing.
var recordPattern = regexp.MustCompile(`^finds: (.+?)\s*$`)

// Over the differences fixture the two disagree, and only where the disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, with its reason on the line above, the original
// makes none the port does not, and every record is matched. The harness's RequireOnlyAdjudicated cannot read this
// fixture: it parses every Go file of the fixture's directory, and the shape it holds is a Go file that does not
// parse, and it reads records only in Go files, where one of the two is in an assembly file. So the records are read
// line by line here, to the harness's rule for a reason, and the findings are the harness's Findings, compared by its
// Difference.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	directory := extractedFixtures(test)
	original, port := parity.Findings(test, buildtag.Analyzer, Analyzer, directory, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	recorded := []parity.Finding{}
	entries, readError := os.ReadDir(filepath.Join(directory, "src", "differences"))
	if readError != nil {
		test.Fatal(readError)
	}
	for _, entry := range entries {
		path := filepath.Join(directory, "src", "differences", entry.Name())
		recorded = append(recorded, recordsIn(test, directory, path)...)
	}
	if len(recorded) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	portAlone := lineOnly(parity.Difference(port, original))
	for _, unrecorded := range parity.Difference(portAlone, recorded) {
		test.Errorf("the port alone found %s, which the fixture does not record", unrecorded)
	}
	for _, unmatched := range parity.Difference(recorded, portAlone) {
		test.Errorf("the fixture records that the port alone finds %s, and it does not", unmatched)
	}
	for _, missing := range parity.Difference(original, port) {
		test.Errorf("the original found %s, and the port did not, a regression", missing)
	}
}

// recordsIn is every finding the fixture file at path, in the tree extracted at directory, records as the port's
// alone, each at its line, the file named as the harness names it. Each record must give its reason on the line
// above, a comment that is no record, as the harness requires of its records; a file here has no declaration whose
// doc could hold one, and an assembly file has none at all.
func recordsIn(test *testing.T, directory string, path string) []parity.Finding {
	test.Helper()
	name := fixtureFileName(directory, path)
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	recorded := []parity.Finding{}
	scanner := bufio.NewScanner(file)
	previous := ""
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		for _, comment := range strings.Split(text, "// port ")[1:] {
			match := recordPattern.FindStringSubmatch(comment)
			if match == nil {
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", name, line, comment)
			}
			reasoned := strings.HasPrefix(strings.TrimSpace(previous), "//") && !strings.Contains(previous, "// port ")
			if !reasoned {
				test.Errorf("%s:%d holds a record with no reason: write why on the line above it", name, line)
			}
			recorded = append(recorded, parity.Finding{File: name, Line: line, Message: match[1]})
		}
		previous = text
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	return recorded
}

// lineOnly is findings by file, line and message alone, for comparing with the differences fixture's records, which
// name a line and a message.
func lineOnly(findings []parity.Finding) []parity.Finding {
	dropped := []parity.Finding{}
	for _, found := range findings {
		dropped = append(dropped, parity.Finding{File: found.File, Line: found.Line, Message: found.Message})
	}
	return dropped
}

// findingAt is diagnostic, reported in loadedPackage from the tree extracted at directory, as the harness records a
// finding, its file named as the harness names it. Neither side relates a position or offers a fix, so a diagnostic
// that does fails here rather than being compared without them.
func findingAt(
	test *testing.T,
	directory string,
	loadedPackage *packages.Package,
	diagnostic analysis.Diagnostic,
) parity.Finding {
	test.Helper()
	if len(diagnostic.Related) > 0 || len(diagnostic.SuggestedFixes) > 0 {
		test.Errorf("the port with no types relates a position or offers a fix, which this comparison does not "+
			"record: %s", diagnostic.Message)
	}
	position := loadedPackage.Fset.Position(diagnostic.Pos)
	found := parity.Finding{
		File:    fixtureFileName(directory, position.Filename),
		Line:    position.Line,
		Column:  position.Column,
		Message: diagnostic.Message,
	}
	if diagnostic.End.IsValid() {
		end := loadedPackage.Fset.Position(diagnostic.End)
		found.EndLine, found.EndColumn = end.Line, end.Column
	}
	return found
}

// fixtureFileName is file named as the harness's Findings names a file of a fixture tree laid out by import path: its
// path below the tree's src, in forward slashes, or its path as it is when it lies outside, so a hand tier's findings
// and records pair with the harness's. The harness keeps that naming in an unexported method of its loads, so it is
// written out here for the one layout this package's fixtures take.
func fixtureFileName(directory string, file string) string {
	below, relativeError := filepath.Rel(filepath.Join(directory, "src"), file)
	if relativeError != nil || strings.HasPrefix(below, "..") {
		return filepath.ToSlash(file)
	}
	return filepath.ToSlash(below)
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

// loadedRoots loads the fixture packages patterns name from directory, each with its test variants, as analysistest
// loads one and the harness's Findings loads them, for the passes the no types tier builds by hand. Every root must
// load clean, and there must be one, by the harness's own judgment of a load.
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
	for _, failure := range parity.LoadFailures(loaded.Roots) {
		test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %s",
			failure.Package, failure.Error)
	}
	return loaded.Roots
}
