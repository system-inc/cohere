package leaky_time_tick

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
	"honnef.co/go/tools/staticcheck/sa1015"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// fixture is one load of fixture packages: the module in a folder of testdata, loaded whole in module mode, so its go
// line sets the version of the standard library its packages expect, or, when module is empty, patterns under
// testdata/src, laid out by import path and loaded as analysistest loads one, whose packages have no version.
type fixture struct {
	module   string
	patterns []string
}

// upstreamFixtures is staticcheck's own fixtures for SA1015, copied from staticcheck/sa1015/testdata/<version> into
// testdata/<version>. Each version's folder holds a go.mod naming that version, the one staticcheck's testutil lays
// over the folder as it runs it, since the version decides the check: from Go 1.23 an unreferenced ticker is
// collected, so the one of Go 1.23 marks nothing, and the main package of Go 1.0 marks nothing by design. One upstream
// file is not as staticcheck ships it: gofmt removed a second blank line before `type T` in
// go1.0/CheckLeakyTimeTick, since the repository's gofmt guard walks testdata, and the line it removed is after every
// mark.
var upstreamFixtures = []fixture{{module: "go1.0"}, {module: "go1.23"}}

// parityFixtures is the upstream fixtures, the parity tree of packages with no version, which holds the shapes the
// check depends on, commands, a program and test files, and three modules of the versions the check reads: Go 1.20,
// whose tagged files have their tag's library, older or newer, Go 1.22, whose files tagged newer have their tag's,
// and Go 1.23.
var parityFixtures = append(slices.Clone(upstreamFixtures),
	fixture{patterns: []string{"parity/..."}},
	fixture{module: "parity_go1.20"},
	fixture{module: "parity_go1.22"},
	fixture{module: "parity_go1.23"},
)

// Staticcheck's own fixtures mark each expected finding with `//@ diag(...)`, read by the shared staticcheck_wants
// reader and matched, one version's module at a time, as staticcheck's test runner matches them, each by one finding
// on its line whose message the mark matches. Every mark the original expects, the port reports, and any finding no
// mark expects fails as much as a mark nothing found, so the port adds nothing there either.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	marks := 0
	for _, upstream := range upstreamFixtures {
		_, port := findingsOf(test, upstream)
		wants, readError := staticcheck_wants.Read(filepath.Join(analysistest.TestData(), upstream.module))
		if readError != nil {
			test.Fatal(readError)
		}
		test.Logf("%s marks %d, the port found %d", upstream.module, len(wants), len(port))
		marks += len(wants)
		findings := []staticcheck_wants.Finding{}
		for _, found := range port {
			// A finding names its file by its path under testdata, which the wants name in full.
			findings = append(findings, staticcheck_wants.Finding{
				File: filepath.Join(analysistest.TestData(), found.file), Line: found.line, Message: found.message,
			})
		}
		unexpected, untaken := staticcheck_wants.Compare(wants, findings)
		for _, found := range unexpected {
			test.Errorf("the port found %s:%d: %s, which no mark expects", found.File, found.Line, found.Message)
		}
		for _, want := range untaken {
			test.Errorf("%s, and the port did not find it", want)
		}
	}
	// The fixture of Go 1.23 marks nothing, by design, so only the two together must mark something.
	if marks == 0 {
		test.Fatal("the upstream fixtures mark nothing, so the comparison proves nothing")
	}
}

// The original and the port run in one checker over the same fixture packages, staticcheck's and parity's, the second
// holding the shapes the check depends on, and their findings must be the same multiset of file, line, column, end
// and message. Each side's count is logged, since two empty sides would match and prove nothing, and the test fails if
// the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, parityFixtures...)
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
// original makes and the port does not, unrecorded, fails, since the port is never worse than the original. Today the
// fixture records nothing, because the two read the same form and differ nowhere: it holds the cases where they could
// plausibly disagree, so the test holds that they agree there, and fails if the original finds nothing in it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, fixture{patterns: []string{"differences"}})
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the comparison proves nothing")
	}
	expectedPort := []finding{}
	expectedOriginal := []finding{}
	relative := filepath.Join("src", "differences", "differences.go")
	path := filepath.Join(analysistest.TestData(), relative)
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
				expectedPort = append(expectedPort, finding{file: relative, line: line, message: match[1]})
			default:
				renamed := finding{file: relative, line: line, message: match[2]}
				expectedOriginal = append(expectedOriginal, renamed)
				expectedPort = append(expectedPort, finding{file: relative, line: line, message: match[3]})
			}
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	test.Logf("the fixture records %d differences", len(expectedPort))
	compareAdjudicated(test, "the port", withoutColumns(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", withoutColumns(difference(original, port)), expectedOriginal)
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

// finding is one finding as the differential compares it. Staticcheck's report gives a finding the end of its call
// as well as its start, so the end is compared too.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
}

// described is found as a finding line prints it.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
}

// findingsOf loads each fixture, runs staticcheck's SA1015 and the port over its packages in one checker, and returns
// each one's findings over them all, sorted.
func findingsOf(test *testing.T, fixtures ...fixture) (original []finding, port []finding) {
	test.Helper()
	for _, each := range fixtures {
		directory := analysistest.TestData()
		patterns := each.patterns
		environment := []string{"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off"}
		if each.module != "" {
			directory = filepath.Join(directory, each.module)
			patterns = []string{"./..."}
			// A module loads as analysistest loads one, in module mode with nothing fetched.
			environment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}
		}
		loaded, loadError := load.Packages(directory, patterns, environment, nil)
		if loadError != nil {
			test.Fatalf("loading %v from %s: %v", patterns, directory, loadError)
		}
		for _, loadedPackage := range loaded.Roots {
			// A fixture that does not type-check is one both sides skip, and would compare nothing.
			if len(loadedPackage.Errors) > 0 {
				test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
			}
		}
		// The packages, not the roots: a package with tests is analyzed once, as its test variant, which holds all
		// its files, as staticcheck's runner analyzes it, where the roots hold both and would report its files twice.
		graph, analyzeError := checker.Analyze(
			[]*analysis.Analyzer{sa1015.Analyzer, Analyzer}, loaded.Packages, &checker.Options{},
		)
		if analyzeError != nil {
			test.Fatalf("analyzing %v from %s: %v", patterns, directory, analyzeError)
		}
		for _, action := range graph.Roots {
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			for _, diagnostic := range action.Diagnostics {
				// Positions as the files were parsed, never as a line directive renames them, so a finding names the
				// fixture file it is in.
				position := action.Package.Fset.PositionFor(diagnostic.Pos, false)
				end := action.Package.Fset.PositionFor(diagnostic.End, false)
				file, relativeError := filepath.Rel(analysistest.TestData(), position.Filename)
				if relativeError != nil {
					test.Fatal(relativeError)
				}
				found := finding{
					file:      file,
					line:      position.Line,
					column:    position.Column,
					endLine:   end.Line,
					endColumn: end.Column,
					message:   diagnostic.Message,
				}
				switch action.Analyzer {
				case sa1015.Analyzer:
					original = append(original, found)
				case Analyzer:
					port = append(port, found)
				}
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
		found.column = 0
		found.endLine = 0
		found.endColumn = 0
		dropped = append(dropped, found)
	}
	return dropped
}
