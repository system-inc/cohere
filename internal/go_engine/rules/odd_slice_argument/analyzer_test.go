package odd_slice_argument

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
	"honnef.co/go/tools/staticcheck/sa5012"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for SA5012, copied unchanged from
// staticcheck/sa5012/testdata/go1.0/CheckEvenSliceLength into testdata/src/CheckEvenSliceLength. The go1.0 folder sets
// the language version of staticcheck's run, which SA5012 never reads, so the fixture loads from a tree laid out by
// import path as the house's fixtures do.
const upstream = "CheckEvenSliceLength"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and every `//@ fact(...)` note, the port exports about the object it names on that line, printed as the
// note says, as staticcheck's testutil checks the original's; the port reports and exports nothing no note expects.
// Its strings.NewReplacer want holds the facts across packages, since the mark comes from the port's run on strings.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	wants, factWants, readError := staticcheck_wants.ReadWithFacts(filepath.Join(analysistest.TestData(), "src",
		upstream))
	if readError != nil {
		test.Fatal(readError)
	}
	if len(wants) == 0 || len(factWants) == 0 {
		test.Fatal("the upstream fixture holds no diag or no fact notes, so the comparison proves nothing")
	}
	graph := analyze(test, []*analysis.Analyzer{Analyzer}, upstream)
	findings := []staticcheck_wants.Finding{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			findings = append(findings, staticcheck_wants.Finding{
				File: position.Filename, Line: position.Line, Message: diagnostic.Message,
			})
		}
	}
	facts := []staticcheck_wants.FoundFact{}
	for _, action := range graph.Roots {
		for _, objectFact := range action.AllObjectFacts() {
			if objectFact.Object.Pkg() != action.Package.Types {
				continue
			}
			position := action.Package.Fset.Position(objectFact.Object.Pos())
			facts = append(facts, staticcheck_wants.FoundFact{
				File: position.Filename, Line: position.Line, Object: objectFact.Object.Name(),
				Fact: fmt.Sprint(objectFact.Fact),
			})
		}
	}
	test.Logf("the fixture wants %d findings and %d facts, the port reported %d and exported %d", len(wants),
		len(factWants), len(findings), len(facts))
	unexpected, untaken := staticcheck_wants.Compare(wants, findings)
	for _, found := range unexpected {
		test.Errorf("%s:%d: the port reported %q, which no note on the line expects", filepath.Base(found.File),
			found.Line, found.Message)
	}
	for _, want := range untaken {
		test.Errorf("the port reported nothing matching %s", want)
	}
	unexpectedFacts, untakenFacts := staticcheck_wants.CompareFacts(factWants, facts)
	for _, found := range unexpectedFacts {
		test.Errorf("%s:%d: the port exported %q about %s, which no note on the line expects",
			filepath.Base(found.File), found.Line, found.Fact, found.Object)
	}
	for _, want := range untakenFacts {
		test.Errorf("the port exported nothing matching %s", want)
	}
}

// The original and the port run in one checker over staticcheck's fixture and parity's packages, parity holding
// every shape of the length check, either way around, checks that mark nothing because they do not dominate every
// return, do not panic, do not divide by two or do not read a length, chains of passing a slice on, sometimes and on
// every path, methods and a method expression, a function of another package, parity/pairs, and every way a slice's
// length is known or is not, with a generated file. Their findings must be the same multiset of file, start, end and
// message, and the marks they export, on every package the checker ran them on, strings among them, the same multiset
// of package, file, line, object and printed fact. Each side's counts are logged, since two empty sides would match
// and prove nothing, and the test fails if the original finds or marks nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	graph := analyze(test, []*analysis.Analyzer{sa5012.Analyzer, Analyzer}, upstream, "parity", "parity/pairs")
	original, port := findingsIn(test, graph)
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
	originalFacts := factsOf(graph, sa5012.Analyzer)
	portFacts := factsOf(graph, Analyzer)
	test.Logf("the original exported %d marks, the port %d", len(originalFacts), len(portFacts))
	if len(originalFacts) == 0 {
		test.Fatal("the original exported no mark, so the comparison of facts proves nothing")
	}
	for _, missing := range difference(originalFacts, portFacts) {
		test.Errorf("the original exported %s, and the port did not", described(missing))
	}
	for _, extra := range difference(portFacts, originalFacts) {
		test.Errorf("the port exported %s, and the original did not", described(extra))
	}
}

// adjudicatedPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and
// the message of a finding only the port makes, or `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?))\s*$`)

// Over the differences fixture the two may disagree only where each disagreement was adjudicated in the port's
// favour, recorded on its line as a `// port finds:` or `// port renames:` comment. The fixture holds the calls where
// the two could most plausibly part, a slice whose length depends on the path taken, one reaching the call through a
// loop's phi, arguments spread over lines and slices of slices of an array, and records none, since the port parts
// from the original nowhere it can run: the test then requires the two to agree there finding for finding and mark
// for mark, and fails if the original finds nothing, so the agreement is over findings and not over silence.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	graph := analyze(test, []*analysis.Analyzer{sa5012.Analyzer, Analyzer}, "differences")
	original, port := findingsIn(test, graph)
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
	originalFacts := factsOf(graph, sa5012.Analyzer)
	portFacts := factsOf(graph, Analyzer)
	if extra := difference(portFacts, originalFacts); len(extra) != 0 {
		test.Errorf("the port alone exported %v", extra)
	}
	if missing := difference(originalFacts, portFacts); len(missing) != 0 {
		test.Errorf("the original alone exported %v", missing)
	}
}

// The one place the two part: a go or defer statement passing an odd slice to a marked parameter, whose call's
// source is the statement, which the original reads as a call expression. The original runs behind a recover over the
// statements fixture and must panic there, losing the package's every finding, and the port reports each odd
// argument inside the statement's call, the two the fixture's go and defer pass, and not the even one.
func TestPortReportsWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	recovering := &analysis.Analyzer{
		Name:      sa5012.Analyzer.Name,
		Doc:       sa5012.Analyzer.Doc,
		Requires:  sa5012.Analyzer.Requires,
		FactTypes: sa5012.Analyzer.FactTypes,
		Run: func(pass *analysis.Pass) (result any, runError error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					runError = fmt.Errorf("the original panicked: %v", recovered)
				}
			}()
			return sa5012.Analyzer.Run(pass)
		},
	}
	graph := analyze(test, []*analysis.Analyzer{recovering, Analyzer}, "statements")
	port := []finding{}
	panicked := false
	for _, action := range graph.Roots {
		switch action.Analyzer {
		case recovering:
			panicked = action.Err != nil
			test.Logf("the original: %v", action.Err)
		case Analyzer:
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			for _, diagnostic := range action.Diagnostics {
				position := action.Package.Fset.Position(diagnostic.Pos)
				end := action.Package.Fset.Position(diagnostic.End)
				port = append(port, finding{
					file: filepath.Base(position.Filename), line: position.Line, column: position.Column,
					endLine: end.Line, endColumn: end.Column, message: diagnostic.Message,
				})
			}
		}
	}
	if !panicked {
		test.Error("the original no longer panics on the statements fixture, so the port's difference there is " +
			"unproven")
	}
	expected := []finding{
		{file: "statements.go", line: 12, column: 16, endLine: 12, endColumn: 17,
			message: `variadic argument "values" is expected to have even number of elements, but has 3 elements`},
		{file: "statements.go", line: 13, column: 19, endLine: 13, endColumn: 20,
			message: `variadic argument "values" is expected to have even number of elements, but has 1 elements`},
	}
	if port = sorted(port); !slices.Equal(port, expected) {
		test.Errorf("the port found %v, where %v was expected", port, expected)
	}
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
// message. A mark is compared as one too: its package and file, where its object is declared, and its object's name
// and the printed fact as its message.
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

// analyze loads the fixture packages patterns name and runs analyzers over them in one checker.
func analyze(test *testing.T, analyzers []*analysis.Analyzer, patterns ...string) *checker.Graph {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		// A fixture that does not type-check is one the checker skips, and would compare nothing.
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	// One variant per package, as staticcheck's runner analyzes, so a package with in-package tests is not judged
	// twice.
	graph, analyzeError := checker.Analyze(analyzers, loaded.Packages, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	return graph
}

// findingsIn is the findings of staticcheck's SA5012 and of the port on the graph's root packages, each sorted.
func findingsIn(test *testing.T, graph *checker.Graph) (original []finding, port []finding) {
	test.Helper()
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
			case sa5012.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	return sorted(original), sorted(port)
}

// factsOf is the marks analyzer exported on every package the graph ran it on, each about an object of that package,
// as findings: the package's path and the file's base name, the object's line and column, and its name and the
// printed fact.
func factsOf(graph *checker.Graph, analyzer *analysis.Analyzer) []finding {
	facts := []finding{}
	for action := range graph.All() {
		if action.Analyzer != analyzer {
			continue
		}
		for _, objectFact := range action.AllObjectFacts() {
			// The action holds the facts it inherited from the packages it imports beside its own.
			if objectFact.Object.Pkg() != action.Package.Types {
				continue
			}
			position := action.Package.Fset.Position(objectFact.Object.Pos())
			facts = append(facts, finding{
				file:    action.Package.PkgPath + "/" + filepath.Base(position.Filename),
				line:    position.Line,
				column:  position.Column,
				message: objectFact.Object.Name() + ": " + fmt.Sprint(objectFact.Fact),
			})
		}
	}
	return sorted(facts)
}

// sorted is findings in the order their descriptions sort.
func sorted(findings []finding) []finding {
	slices.SortFunc(findings, func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	})
	return findings
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
