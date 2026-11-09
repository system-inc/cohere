package build_intermediate_representation

import (
	"go/ast"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// SourceFunctions lists the fixture's functions in the order buildir's SrcFuncs lists them, read in one checker beside
// buildir: the synthetic initializer, the declared functions and methods as declared, then the anonymous functions,
// depth first, a closure's closures before the next closure of its parent. The fixture holds every place a source
// function comes from, so the order is held at each.
func TestSourceFunctionsReadInBuildirsOrder(test *testing.T) {
	test.Parallel()
	theirs := &analysis.Analyzer{
		Name:       "their_order",
		Doc:        "names buildir's source functions in order",
		Requires:   []*analysis.Analyzer{staticcheck_ir.Analyzer},
		ResultType: reflect.TypeFor[[]string](),
		Run: func(pass *analysis.Pass) (any, error) {
			functions, readError := staticcheck_ir.Functions(pass)
			names := []string{}
			for _, function := range functions {
				names = append(names, function.RelString(nil))
			}
			return names, readError
		},
	}
	ours := &analysis.Analyzer{
		Name:       "our_order",
		Doc:        "names our source functions in order",
		Requires:   []*analysis.Analyzer{Analyzer},
		ResultType: reflect.TypeFor[[]string](),
		Run: func(pass *analysis.Pass) (any, error) {
			names := []string{}
			for function := range mustResultOf(pass).SourceFunctions(nil, "") {
				names = append(names, function.RelativeString(nil))
			}
			return names, nil
		},
	}
	results := rootResults(test, &checker.Options{}, theirs, ours)
	theirNames, isTheirs := results[theirs].([]string)
	ourNames, isOurs := results[ours].([]string)
	if !isTheirs || !isOurs {
		test.Fatalf("the probes returned %T and %T, where two lists of names were expected", results[theirs],
			results[ours])
	}
	anonymous := 0
	for _, name := range theirNames {
		if strings.Contains(name, "$") {
			anonymous++
		}
	}
	if len(theirNames) < 12 || anonymous < 6 {
		test.Fatalf("buildir read the fixture as %d functions, %d of them anonymous, too few to hold the order: %q",
			len(theirNames), anonymous, theirNames)
	}
	if !slices.Equal(ourNames, theirNames) {
		test.Errorf("our source functions read\n%s\nwhere buildir's read\n%s", strings.Join(ourNames, "\n"),
			strings.Join(theirNames, "\n"))
	}
}

// Analyzer builds nothing, and SourceFunctions builds a function only as it is read: a reader that stops after the
// first function leaves every other one without blocks, and one that reads them all finds each built.
func TestFunctionsBuildOnlyWhenRead(test *testing.T) {
	test.Parallel()
	type reading struct {
		unbuiltBefore int
		unbuiltAfter  int
		functions     int
		emptied       []string
	}
	probe := &analysis.Analyzer{
		Name:       "build_probe",
		Doc:        "counts the functions built before and after reading one, then all",
		Requires:   []*analysis.Analyzer{Analyzer},
		ResultType: reflect.TypeFor[reading](),
		Run: func(pass *analysis.Pass) (any, error) {
			result := mustResultOf(pass)
			read := reading{functions: len(result.Package.Functions)}
			unbuilt := func() int {
				count := 0
				for _, function := range result.Package.Functions {
					if len(function.Blocks) == 0 {
						count++
					}
				}
				return count
			}
			read.unbuiltBefore = unbuilt()
			for range result.SourceFunctions(nil, "") {
				break
			}
			read.unbuiltAfter = unbuilt()
			for function := range result.SourceFunctions(nil, "") {
				if len(function.Blocks) == 0 {
					read.emptied = append(read.emptied, function.RelativeString(nil))
				}
			}
			return read, nil
		},
	}
	read, isReading := rootResults(test, &checker.Options{}, probe)[probe].(reading)
	if !isReading {
		test.Fatal("the probe returned no reading")
	}
	if read.functions < 8 || read.unbuiltBefore != read.functions || read.unbuiltAfter != read.functions-1 {
		test.Errorf("of %d package functions, %d were unbuilt before any was read and %d after the first was, "+
			"where all of them and all but one were expected", read.functions, read.unbuiltBefore, read.unbuiltAfter)
	}
	if len(read.emptied) != 0 {
		test.Errorf("read through SourceFunctions, %q had no blocks, where each was built as it was read",
			read.emptied)
	}
}

// The expression map is recorded for a package exactly when an analyzer requiring ExpressionReader requires Analyzer
// there. Under the engine's Dependents, a reader of the map finds values for the fixture's expressions, and a reader
// of the representation alone finds none, alone or beside the map's reader, which shares its one result and so finds
// them too. Under a driver with no Dependents, every package records it, as buildir's GlobalDebug does.
func TestTheExpressionMapIsRecordedForItsReaders(test *testing.T) {
	test.Parallel()
	expressionProbe := func(name string, requires ...*analysis.Analyzer) *analysis.Analyzer {
		return &analysis.Analyzer{
			Name:       name,
			Doc:        "counts the expressions the fixture's functions map to a value",
			Requires:   requires,
			ResultType: reflect.TypeFor[int](),
			Run: func(pass *analysis.Pass) (any, error) {
				return mappedExpressions(mustResultOf(pass)), nil
			},
		}
	}
	reader := expressionProbe("map_reader", Analyzer, ExpressionReader)
	other := expressionProbe("other_reader", Analyzer)
	withDependents := &checker.Options{Dependents: rule.Dependents}
	cases := []struct {
		name      string
		options   *checker.Options
		analyzers []*analysis.Analyzer
		mapped    map[*analysis.Analyzer]bool
	}{
		{"the map's reader", withDependents, []*analysis.Analyzer{reader}, map[*analysis.Analyzer]bool{reader: true}},
		{"another reader", withDependents, []*analysis.Analyzer{other}, map[*analysis.Analyzer]bool{other: false}},
		{"both", withDependents, []*analysis.Analyzer{reader, other},
			map[*analysis.Analyzer]bool{reader: true, other: true}},
		{"no dependents", &checker.Options{}, []*analysis.Analyzer{other}, map[*analysis.Analyzer]bool{other: true}},
	}
	for _, testCase := range cases {
		results := rootResults(test, testCase.options, testCase.analyzers...)
		for analyzer, expected := range testCase.mapped {
			count, isCount := results[analyzer].(int)
			if !isCount || (count > 0) != expected {
				test.Errorf("with %s, %s found %v mapped expressions, where any was %t", testCase.name,
					analyzer.Name, results[analyzer], expected)
			}
		}
	}
}

// mappedExpressions is how many expressions in the syntax of result's source functions map to a value.
func mappedExpressions(result *Result) int {
	count := 0
	for function := range result.SourceFunctions(nil, "") {
		syntax := function.Syntax()
		if syntax == nil || function.Parent() != nil {
			continue
		}
		ast.Inspect(syntax, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if isExpression {
				if value, _ := function.ValueForExpression(expression); value != nil {
					count++
				}
			}
			return true
		})
	}
	return count
}

// mustResultOf is Analyzer's result in pass, which every probe here requires.
func mustResultOf(pass *analysis.Pass) *Result {
	result, readError := ResultOf(pass)
	if readError != nil {
		// Invariant: every probe here requires Analyzer, whose run returns a *Result or fails the probe before it runs.
		panic(readError)
	}
	return result
}

// rootResults runs analyzers over the functions fixture in one checker under options, and returns each one's result
// on it, failing the test on an action that failed.
func rootResults(
	test *testing.T,
	options *checker.Options,
	analyzers ...*analysis.Analyzer,
) map[*analysis.Analyzer]any {
	test.Helper()
	directory := analysistest.TestData()
	// The fixture is a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, []string{"functions"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	graph, analyzeError := checker.Analyze(analyzers, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	results := map[*analysis.Analyzer]any{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		results[action.Analyzer] = action.Result
	}
	return results
}
