package cgo_call

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
	"golang.org/x/tools/go/analysis/passes/cgocall"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for cgocall, copied unchanged into testdata/src and run as x/tools' test runs them, with no
// flags: every `// want` the original expects, the port reports at the same line with the same message, and
// analysistest fails on a finding no want expects as much as on a want nothing found, so the port adds nothing there
// either. a and typeparams have cgo of their own, so the load has the cgo tool translate them, and the pass judges
// them through the translation's `//line` directives; b and c name a C that is not cgo, and import no runtime/cgo.
// The translation needs a go that compiles and a toolchain with cgo, so the test skips, named, where either is
// missing, as every test of the engine that translates cgo does.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, "a", "b", "c", "typeparams")
}

// upstreamPackages are x/tools' fixture packages, every one its test runs.
var upstreamPackages = []string{"a", "b", "c", "typeparams"}

// parityPackages are the fixture packages written for the differential, each a path of the original's through a
// package, and illTyped those of them that hold type errors on purpose: parity, a cgo package the cgo tool
// translates, with every branch of the judgment; illtyped, one whose type errors leave an address untyped beside a
// typed value; faked, one the cgo tool fails on, so the load type-checks it from its files as written with package
// "C" faked; and runtimecgo, one that imports runtime/cgo with no file importing "C".
var (
	parityPackages = []string{"parity", "illtyped", "faked", "runtimecgo"}
	illTyped       = map[string]bool{"illtyped": true, "faked": true, "differences": true}
)

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, and their
// findings must be the same multiset of file, line, column and message. Each side's count is logged, since two empty
// sides would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)
	compared := findingsOf(test, slices.Concat(upstreamPackages, parityPackages)...)
	test.Logf("the original found %d, the port %d", len(compared.original), len(compared.port))
	for _, failure := range compared.originalFailures {
		test.Errorf("the original failed: %v", failure)
	}
	if len(compared.original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(compared.original, compared.port) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
	for _, extra := range difference(compared.port, compared.original) {
		test.Errorf("the port found %s, and the original did not", described(extra))
	}
}

// adjudicatedPattern is one comment in the differences fixture, the message after its `// port finds: ` opening, of
// a finding only the port makes.
var adjudicatedPattern = regexp.MustCompile(`// port finds: (.+?)\s*$`)

// refusedImportPanic is the original's panic over the differences fixture, its unchecked assertion of the package
// name the type checker never recorded for the refused import.
const refusedImportPanic = "interface conversion: types.Object is nil, not *types.PkgName"

// Over the differences fixture the two disagree, and only where the disagreement was adjudicated in the port's
// favour: the original panics over its refused import and finds nothing, and the port finds exactly what the fixture
// records on each line. A finding the original makes, or a failure other than that panic, fails, since the port is
// never worse than the original.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)
	compared := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(compared.original), len(compared.port))
	if len(compared.originalFailures) != 1 ||
		!strings.Contains(compared.originalFailures[0].Error(), refusedImportPanic) {
		test.Errorf("expected the original to fail once, panicking with %q, got %v", refusedImportPanic,
			compared.originalFailures)
	}
	for _, found := range compared.original {
		test.Errorf("the original found %s, and is expected to find nothing", described(found))
	}
	expected := []finding{}
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
		if match := adjudicatedPattern.FindStringSubmatch(scanner.Text()); match != nil {
			expected = append(expected, finding{file: "differences.go", line: line, message: match[1]})
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	if len(expected) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	actual := []finding{}
	for _, found := range compared.port {
		actual = append(actual, finding{file: found.file, line: found.line, message: found.message})
	}
	for _, unrecorded := range difference(actual, expected) {
		test.Errorf("the port alone found %s, which the fixture does not record", described(unrecorded))
	}
	for _, unmatched := range difference(expected, actual) {
		test.Errorf("the fixture records that the port alone finds %s, and it does not", described(unmatched))
	}
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

// comparison is what the original and the port found over the same packages, each side's findings sorted, and the
// failures of the original's runs, each a panic recovered.
type comparison struct {
	original         []finding
	port             []finding
	originalFailures []error
}

// recoveringOriginal is x/tools' cgocall with a recover around its Run, so a panic of the original over a fixture
// is a failure of its action the test reads, not the end of the test binary.
var recoveringOriginal = func() *analysis.Analyzer {
	recovering := *cgocall.Analyzer
	recovering.Run = func(pass *analysis.Pass) (result any, runError error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				runError = fmt.Errorf("the original panicked on %s: %v", pass.Pkg.Path(), recovered)
			}
		}()
		return cgocall.Analyzer.Run(pass)
	}
	return &recovering
}()

// findingsOf loads the fixture packages patterns name, runs x/tools' cgocall and the port over them in one checker,
// and returns what each found.
func findingsOf(test *testing.T, patterns ...string) comparison {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	if len(loaded.Roots) != len(patterns) {
		test.Fatalf("loading %v found %d packages, expected one a pattern", patterns, len(loaded.Roots))
	}
	// Both run despite type errors, so a fixture that does not type-check is still compared. Each must be as its
	// pattern says, so a fixture meant to hold type errors proves the path it was written for, and one meant to be
	// clean is not compared with half its types missing.
	for _, root := range loaded.Roots {
		if loadsClean := len(root.Errors) == 0; loadsClean == illTyped[root.PkgPath] {
			test.Fatalf("fixture package %s loaded with errors %v, expected errors: %t", root.ID, root.Errors,
				illTyped[root.PkgPath])
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{recoveringOriginal, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	compared := comparison{}
	for _, action := range graph.Roots {
		switch {
		case action.Err != nil && action.Analyzer == recoveringOriginal:
			compared.originalFailures = append(compared.originalFailures, action.Err)
			continue
		case action.Err != nil:
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
			case recoveringOriginal:
				compared.original = append(compared.original, found)
			case Analyzer:
				compared.port = append(compared.port, found)
			}
		}
	}
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(compared.original, compare)
	slices.SortFunc(compared.port, compare)
	return compared
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
// Loading the fixtures needs go list, and the cgo tool through go list -compiled, which cgo_testing.RequireTranslation
// asks for first.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
