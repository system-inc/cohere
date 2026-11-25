package typed_nil_interface_comparison_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/rules/function_triviality"
	"github.com/system-inc/cohere/internal/go_language/rules/return_nil_ness"
	"github.com/system-inc/cohere/internal/go_language/rules/staticcheck_report"
	"github.com/system-inc/cohere/internal/go_language/rules/typed_nil_interface_comparison"
)

// everyFixture loads every package under the rule's testdata: staticcheck's own fixtures, parity's with the producer
// its facts cross from, the differences and the test file.
var everyFixture = []string{"go1.0/...", "go1.9/...", "go1.18/...", "parity/...", "differences", "testfile"}

// packageLevel is the unit a finding outside every function declaration belongs to, the package's initializer.
const packageLevel = "the package's level"

// loadEveryFixture loads everyFixture, each package clean, since the checker skips one that does not type-check.
func loadEveryFixture(test *testing.T) []*packages.Package {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, everyFixture, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the rule's fixtures: %v", loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatal("loading the rule's fixtures found no package")
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("the fixture %s does not load clean: %v", root.ID, root.Errors)
		}
	}
	return loaded.Roots
}

// placeAt is position as two loads of the same files name it alike: the file and the offset in it.
func placeAt(fileSet *token.FileSet, position token.Pos) string {
	if !position.IsValid() {
		return "nowhere"
	}
	placed := fileSet.PositionFor(position, false)
	return fmt.Sprintf("%s:%d", placed.Filename, placed.Offset)
}

// findingLine is diagnostic as one line, whole: its range, category and message, and each related place with its
// message, in order.
func findingLine(fileSet *token.FileSet, diagnostic analysis.Diagnostic) string {
	line := fmt.Sprintf("%s to %s %q %q", placeAt(fileSet, diagnostic.Pos), placeAt(fileSet, diagnostic.End),
		diagnostic.Category, diagnostic.Message)
	for _, related := range diagnostic.Related {
		line += fmt.Sprintf(" | %s to %s %q", placeAt(fileSet, related.Pos), placeAt(fileSet, related.End),
			related.Message)
	}
	return line
}

// declarationHolding is the place of the function declaration of analyzed that holds position, or packageLevel for a
// position outside every one.
func declarationHolding(analyzed *packages.Package, position token.Pos) string {
	for _, file := range analyzed.Syntax {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if isFunction && function.Pos() <= position && position < function.End() {
				return placeAt(analyzed.Fset, function.Pos())
			}
		}
	}
	return packageLevel
}

// findingsWhole runs the rule over roots as the engine does, the package built whole before the walk, and is its
// findings, by package ID and then by the function declaration each lies in.
func findingsWhole(test *testing.T, roots []*packages.Package) map[string]map[string][]string {
	test.Helper()
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{typed_nil_interface_comparison.Analyzer}, roots,
		check.DispatchOptions())
	if analyzeError != nil {
		test.Fatalf("analyzing the fixtures whole: %v", analyzeError)
	}
	found := map[string]map[string][]string{}
	for _, action := range graph.Roots {
		if action.Analyzer != typed_nil_interface_comparison.Analyzer {
			continue
		}
		if action.Err != nil {
			test.Fatalf("running the rule on %s: %v", action.Package.ID, action.Err)
		}
		found[action.Package.ID] = map[string][]string{}
		for _, diagnostic := range action.Diagnostics {
			unit := declarationHolding(action.Package, diagnostic.Pos)
			found[action.Package.ID][unit] = append(found[action.Package.ID][unit],
				findingLine(action.Package.Fset, diagnostic))
		}
	}
	return found
}

// aloneResult is what the rule found in one package judged one function at a time: its findings by the function
// declaration judged when each was reported, and what went wrong.
type aloneResult struct {
	findings map[string][]string
	problems []string
}

// eachWithAnonymous calls visit on function and on every anonymous function below it, depth first.
func eachWithAnonymous(
	function *intermediate_representation.Function,
	visit func(*intermediate_representation.Function),
) {
	visit(function)
	for _, anonymous := range function.AnonymousFunctions {
		eachWithAnonymous(anonymous, visit)
	}
}

// newAloneAnalyzer is the rule's check run one function at a time: it creates the package as the readers' is created,
// with nothing built, and for each top-level function in turn builds only it, with the function literals in it, and
// runs Check on those. It never requires build_intermediate_representation, which builds a package whole. A function
// found built before its turn is a problem: something judged earlier built a function it does not judge.
func newAloneAnalyzer() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "SA4023",
		Doc:  "runs SA4023's check on each function with only that function built",
		Requires: []*analysis.Analyzer{ctrlflow.Analyzer, generated.Analyzer, return_nil_ness.Analyzer,
			function_triviality.Analyzer},
		ResultType: reflect.TypeFor[aloneResult](),
		Run: func(pass *analysis.Pass) (any, error) {
			context, contextError := staticcheck_report.PassContext(pass)
			if contextError != nil {
				return nil, contextError
			}
			graphs, isGraphs := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
			nilnesses, isNilnesses := pass.ResultOf[return_nil_ness.Analyzer].(*return_nil_ness.Result)
			trivialities, isTrivialities := pass.ResultOf[function_triviality.Analyzer].(*function_triviality.Result)
			if !isGraphs || !isNilnesses || !isTrivialities {
				return nil, fmt.Errorf("a result the check reads is missing on %s", pass.Pkg.Path())
			}
			created := build_intermediate_representation.NewPackage(pass.Fset, pass.Pkg, pass.Files, pass.TypesInfo,
				graphs.NoReturn, false)
			alone := aloneResult{findings: map[string][]string{}}
			judging := packageLevel
			report := pass.Report
			pass.Report = func(diagnostic analysis.Diagnostic) {
				alone.findings[judging] = append(alone.findings[judging], findingLine(pass.Fset, diagnostic))
				report(diagnostic)
			}
			for _, function := range created.Functions {
				if function.Blocks != nil {
					alone.problems = append(alone.problems, fmt.Sprintf("%s was built before its turn", function))
				}
				judging = packageLevel
				if function.Syntax() != nil {
					judging = placeAt(pass.Fset, function.Syntax().Pos())
				}
				function.Build()
				eachWithAnonymous(function, func(judged *intermediate_representation.Function) {
					typed_nil_interface_comparison.Check(context, nilnesses.Asking(context.RuleName()),
						trivialities.Asking(context.RuleName()), judged)
				})
			}
			return alone, nil
		},
	}
}

// The rule needs no function nobody built (#h0k644s): with no function of a package built up front, and each built
// only for its own turn, the check finds in every function what the whole run finds there, range, message and related
// places alike, over every fixture of the rule. What it reads of another function it asks of return_nil_ness and
// function_triviality, which build what their answers read in the resolver's own package, so no function of the
// package the check walks is ever built for another's judgment.
func TestEachFunctionJudgedAloneFindsWhatTheWholeRunFinds(test *testing.T) {
	test.Parallel()
	whole := findingsWhole(test, loadEveryFixture(test))
	alone := newAloneAnalyzer()
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{alone}, loadEveryFixture(test), options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixtures one function at a time: %v", analyzeError)
	}
	compared, onCallees := 0, 0
	judged := map[string]bool{}
	for _, action := range graph.Roots {
		if action.Analyzer != alone {
			continue
		}
		found, isFound := action.Result.(aloneResult)
		if action.Err != nil || !isFound {
			test.Fatalf("judging %s one function at a time: %v", action.Package.ID, action.Err)
		}
		for _, problem := range found.problems {
			test.Errorf("%s: %s", action.Package.ID, problem)
		}
		judged[action.Package.ID] = true
		wholeFound, isWhole := whole[action.Package.ID]
		if !isWhole {
			test.Errorf("the whole run never ran the rule on %s", action.Package.ID)
			continue
		}
		units := slices.Sorted(maps.Keys(wholeFound))
		for _, unit := range slices.Sorted(maps.Keys(found.findings)) {
			if _, isListed := wholeFound[unit]; !isListed {
				units = append(units, unit)
			}
		}
		for _, unit := range units {
			wholeLines, aloneLines := slices.Sorted(slices.Values(wholeFound[unit])),
				slices.Sorted(slices.Values(found.findings[unit]))
			if !slices.Equal(wholeLines, aloneLines) {
				test.Errorf("%s, in the function at %s: the whole run finds %q, the function judged alone %q",
					action.Package.ID, unit, wholeLines, aloneLines)
			}
			compared += len(wholeLines)
			for _, line := range wholeLines {
				if strings.Contains(line, "never returns a nil interface value") {
					onCallees++
				}
			}
		}
	}
	for identifier := range whole {
		if !judged[identifier] {
			test.Errorf("%s was never judged one function at a time", identifier)
		}
	}
	if compared == 0 || onCallees == 0 {
		test.Errorf("compared %d findings, %d of them resting on a callee, so the test proves nothing", compared,
			onCallees)
	}
	if computed := resolver.Counts(function_triviality.Kind).Computed.Load(); computed == 0 {
		test.Error("function_triviality judged no function, so no finding judged alone rested on its answer")
	}
	test.Logf("compared %d findings over %d packages, %d resting on a callee; function_triviality judged %d functions",
		compared, len(judged), onCallees, resolver.Counts(function_triviality.Kind).Computed.Load())
}

// A comparison in a test file is never reported, so the rule never asks whether its callee is trivial either: over the
// testfile fixture, function_triviality judges no function.
func TestAComparisonInATestFileAsksNoTriviality(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"testfile"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil || len(loaded.Roots) == 0 {
		test.Fatalf("loading the testfile fixture: %v", loadError)
	}
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{typed_nil_interface_comparison.Analyzer},
		loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the testfile fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Analyzer == typed_nil_interface_comparison.Analyzer && action.Err != nil {
			test.Fatalf("running the rule on %s: %v", action.Package, action.Err)
		}
	}
	if computed := resolver.Counts(function_triviality.Kind).Computed.Load(); computed != 0 {
		test.Errorf("function_triviality judged %d functions for comparisons in a test file, want 0", computed)
	}
}
