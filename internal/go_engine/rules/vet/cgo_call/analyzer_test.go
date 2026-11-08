package cgo_call

import (
	"bufio"
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
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for cgocall, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more. a and typeparams have cgo of their own, so the load has the cgo tool translate them,
// and the pass judges them through the translation's `//line` directives; b and c name a C that is not cgo, and
// import no runtime/cgo. The translation needs a go that compiles and a toolchain with cgo, so the test skips, named,
// where either is missing, as every test of the engine that translates cgo does.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "b", "c", "typeparams")
}

// Over x/tools' fixtures and the clean parity fixtures, parity, a cgo package the cgo tool translates, with every
// branch of the judgment, and runtimecgo, one that imports runtime/cgo with no file importing "C", the original and
// the port find the same findings, each the same in every field.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)
	parity.RequireIdentical(test, cgocall.Analyzer, Analyzer, analysistest.TestData(), "a", "b", "c", "typeparams",
		"parity", "runtimecgo")
}

// illTypedLoadErrors are the errors the illtyped fixture loads with, each one it is written for: the type checker
// refusing each address its Untyped takes, of a map's element as the cgo tool rewrites the map, of a call's result,
// and of a name never declared.
var illTypedLoadErrors = []parity.LoadError{
	{
		Position: "illtyped/illtyped.go:15:25",
		Message: `invalid operation: cannot take address of (*_cgoIndex0)["key"] (map index expression of type ` +
			`[]int)`,
	},
	{
		Position: "illtyped/illtyped.go:16:10",
		Message: `invalid operation: cannot take address of (*_cgoIndex0)["key"] (map index expression of type ` +
			`[]int)`,
	},
	{
		Position: "illtyped/illtyped.go:18:25",
		Message:  `invalid operation: cannot take address of (*_cgoIndex0)["key"] (map index expression of type int)`,
	},
	{
		Position: "illtyped/illtyped.go:19:25",
		Message:  "invalid operation: cannot take address of Values() (value of type []int)",
	},
	{
		Position: "illtyped/illtyped.go:20:25",
		Message:  "invalid operation: cannot take address of Number() (value of type int)",
	},
	{Position: "illtyped/illtyped.go:21:25", Message: "undefined: undeclared"},
	{
		Position: "illtyped/illtyped.go:23:52",
		Message: `invalid operation: cannot take address of (*_cgoIndex1)["key"] (map index expression of type ` +
			`[]int)`,
	},
}

// Over illtyped, a parity fixture whose type errors leave an address untyped beside a typed value, the original and
// the port find the same findings, each the same in every field.
func TestPortMatchesOriginalOverIllTypedFixture(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)
	parity.RequireIdenticalDespiteLoadErrors(test, cgocall.Analyzer, Analyzer, analysistest.TestData(),
		illTypedLoadErrors, "illtyped")
}

// Over faked, a parity fixture the cgo tool fails on, so the load type-checks it from its files as written with
// package "C" faked, the original and the port find the same findings, each the same in every field, and the original
// runs on it without failing.
//
// RequireIdenticalDespiteLoadErrors would list faked's one load error exactly, and that error is the C compiler's own
// text, failing on the header the fixture includes, which does not exist: clang and gcc word it differently, so a
// list that pins either would fail on a box with the other. So this tier keeps its own load, which holds the fixture
// to failing to load without reading the words, and compares through the harness's Analyze and Difference.
func TestPortMatchesOriginalOverFakedFixture(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)
	outcome := illTypedOutcome(test, "faked")
	test.Logf("the original found %d, the port %d", len(outcome.Original.Findings), len(outcome.Port.Findings))
	for _, failure := range outcome.Original.Failures {
		test.Errorf("the original failed on %s: %s", failure.Package, failure.Error)
	}
	if len(outcome.Original.Findings) == 0 {
		test.Fatal("the original found nothing in the fixture, so the comparison proves nothing")
	}
	for _, missing := range parity.Difference(outcome.Original.Findings, outcome.Port.Findings) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
	for _, extra := range parity.Difference(outcome.Port.Findings, outcome.Original.Findings) {
		test.Errorf("the port found %s, and the original did not", extra)
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
//
// The harness's form for a fixture the original panics on, RequirePortWhereOriginalPanics, loads it clean, and this
// one holds a type error on purpose, the refused import the panic comes from, so the harness refuses it before
// running either side. So this tier keeps its own load and its reading of the records, and compares through the
// harness's Analyze, Finding and Difference.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)
	outcome := illTypedOutcome(test, "differences")
	test.Logf("the original found %d, the port %d", len(outcome.Original.Findings), len(outcome.Port.Findings))
	if len(outcome.Original.Failures) != 1 ||
		!strings.Contains(outcome.Original.Failures[0].Error, refusedImportPanic) {
		test.Errorf("expected the original to fail once, panicking with %q, got %v", refusedImportPanic,
			outcome.Original.Failures)
	}
	for _, found := range outcome.Original.Findings {
		test.Errorf("the original found %s, and is expected to find nothing", found)
	}
	expected := []parity.Finding{}
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
			expected = append(expected, parity.Finding{File: "differences.go", Line: line, Message: match[1]})
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	if len(expected) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	// A record names a line and a message, so the port's findings are matched by those alone.
	actual := []parity.Finding{}
	for _, found := range outcome.Port.Findings {
		actual = append(actual, parity.Finding{File: found.File, Line: found.Line, Message: found.Message})
	}
	for _, unrecorded := range parity.Difference(actual, expected) {
		test.Errorf("the port alone found %s, which the fixture does not record", unrecorded)
	}
	for _, unmatched := range parity.Difference(expected, actual) {
		test.Errorf("the fixture records that the port alone finds %s, and it does not", unmatched)
	}
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

// illTypedOutcome loads the fixture packages patterns name, each of which must load with errors, a type error or the
// cgo tool's failure, so it proves the path it was written for, and runs the recovering original and the port over
// them in one checker, through the harness's Analyze, each side's findings in their files' base names. The port
// failing on one fails the test.
func illTypedOutcome(test *testing.T, patterns ...string) parity.Outcome {
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
	// Both run despite errors, so a fixture that does not load clean is still compared, and each of these must not,
	// since one that loads clean would not prove the path it was written for.
	failed := parity.LoadFailures(loaded.Roots)
	for _, root := range loaded.Roots {
		if !slices.ContainsFunc(failed, func(failure parity.Failure) bool { return failure.Package == root.ID }) {
			test.Fatalf("fixture package %s loaded clean, where it is written to hold type errors", root.ID)
		}
	}
	outcomes, analyzeError := parity.Analyze(
		[]parity.Pair{{Name: "cgocall", Original: recoveringOriginal, Port: Analyzer}}, loaded.Roots,
	)
	if analyzeError != nil {
		test.Fatalf("%v", analyzeError)
	}
	outcome := outcomes[0]
	for _, failure := range outcome.Port.Failures {
		test.Fatalf("the port failed on %s: %s", failure.Package, failure.Error)
	}
	for _, side := range []*parity.Side{&outcome.Original, &outcome.Port} {
		for index, found := range side.Findings {
			side.Findings[index] = found.WithFileNames(filepath.Base)
		}
	}
	return outcome
}
