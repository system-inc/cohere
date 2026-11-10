package build_intermediate_representation

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// SourceFunctions lists the fixture's functions in the order buildir's SrcFuncs lists them, read in one checker beside
// buildirsRun: the synthetic initializer, the declared functions and methods as declared, then the anonymous functions,
// depth first, a closure's closures before the next closure of its parent. The fixture holds every place a source
// function comes from, so the order is held at each.
func TestSourceFunctionsReadInBuildirsOrder(test *testing.T) {
	test.Parallel()
	theirs := &analysis.Analyzer{
		Name:       "their_order",
		Doc:        "names buildir's source functions in order",
		Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
		ResultType: reflect.TypeFor[[]string](),
		Run: func(pass *analysis.Pass) (any, error) {
			names := []string{}
			for _, function := range buildirsRun(pass) {
				names = append(names, function.RelString(nil))
			}
			return names, nil
		},
	}
	ours := &analysis.Analyzer{
		Name:       "our_order",
		Doc:        "names our source functions in order",
		Requires:   []*analysis.Analyzer{Analyzer},
		ResultType: reflect.TypeFor[[]string](),
		Run: func(pass *analysis.Pass) (any, error) {
			names := []string{}
			for function := range mustResultOf(pass).SourceFunctions() {
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

// buildirsRun is the source functions staticcheck's buildir pass lists in SrcFuncs for pass's package, built as its run
// builds them (honnef.co/go/tools@v0.8.1 internal/passes/buildir/buildir.go), which is internal to staticcheck's
// module, so cohere cannot run the pass itself: a program for the package alone in GlobalDebug mode with ctrlflow's
// no-return predicate, the package's functions as created, then each one's anonymous functions, depth first. pass's
// analyzer must require ctrlflow.
func buildirsRun(pass *analysis.Pass) []*ir.Function {
	controlFlow, isControlFlow := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	if !isControlFlow {
		// Invariant: the probe requires ctrlflow, whose result is always its CFGs.
		panic(fmt.Sprintf("ctrlflow's result is %T", pass.ResultOf[ctrlflow.Analyzer]))
	}
	program := ir.NewProgram(pass.Fset, ir.GlobalDebug)
	program.SetNoReturn(controlFlow.NoReturn)
	for _, imported := range pass.Pkg.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	built := program.CreatePackage(pass.Pkg, pass.Files, pass.TypesInfo, false)
	built.Build()
	functions := slices.Clone(built.Functions)
	var addAnonymous func(function *ir.Function)
	addAnonymous = func(function *ir.Function) {
		for _, anonymous := range function.AnonFuncs {
			functions = append(functions, anonymous)
			addAnonymous(anonymous)
		}
	}
	for _, function := range built.Functions {
		addAnonymous(function)
	}
	return functions
}

// Analyzer builds the whole package as its own action, before any reader of the package starts, as buildir built it
// before any check ran (#zrfcwdy): a reader finds every function built, anonymous ones included, before it reads the
// result at all, so no reader builds the package inside its own action while another waits on that build. Building
// one function at a time, as each is read, is the intermediate representation package's own laziness, held there
// (TestBuildingOneFunctionBuildsOnlyIt), and the fact resolver's, and not what a reader of this result gets.
//
// It holds on both of run's paths: the package run creates alone, under a driver with no resolver, and the package the
// run's resolver shares, which the resolver creates unbuilt when no question of its own reached the package first, so
// run must build it as surely as one it created.
func TestEveryFunctionIsBuiltBeforeAReaderStarts(test *testing.T) {
	test.Parallel()
	type reading struct {
		shared    bool
		functions int
		unbuilt   []string
	}
	probe := &analysis.Analyzer{
		Name:       "build_probe",
		Doc:        "names each function that had no blocks as the reader started",
		Requires:   []*analysis.Analyzer{Analyzer},
		ResultType: reflect.TypeFor[reading](),
		Run: func(pass *analysis.Pass) (any, error) {
			result := mustResultOf(pass)
			resolver, isSharing := pass.ResultOf[rule.Resolved].(*sharingResolver)
			// Read off the package itself, not through Package or SourceFunctions, so a build either did would not
			// count.
			every := withAnonymous(result.irPackage.Functions)
			read := reading{
				shared:    isSharing && result.irPackage == resolver.handedOut(pass.Pkg),
				functions: len(every),
			}
			for _, function := range every {
				if len(function.Blocks) == 0 {
					read.unbuilt = append(read.unbuilt, function.RelativeString(nil))
				}
			}
			return read, nil
		},
	}
	cases := []struct {
		name    string
		options *checker.Options
		shared  bool
	}{
		{"created alone", &checker.Options{}, false},
		{"shared by the resolver", &checker.Options{Resolver: newSharingResolver(), Resolved: rule.Resolved}, true},
	}
	for _, testCase := range cases {
		read, isReading := rootResults(test, testCase.options, probe)[probe].(reading)
		if !isReading {
			test.Fatalf("%s, the probe returned no reading", testCase.name)
		}
		if read.shared != testCase.shared {
			test.Errorf("%s, the reader's package was the one the resolver handed out %t, where %t was expected",
				testCase.name, read.shared, testCase.shared)
		}
		if read.functions < 8 {
			test.Fatalf("%s, the fixture held %d functions, where at least 8 were expected", testCase.name,
				read.functions)
		}
		if len(read.unbuilt) != 0 {
			test.Errorf("%s, as the reader started, %q of %d functions had no blocks, where the whole package was "+
				"expected built by %s's own action", testCase.name, read.unbuilt, read.functions, Analyzer.Name)
		}
	}
}

// sharingResolver stands in for the run's fact resolver, which this package's tests cannot import, since it imports
// this package. As the resolver does, it learns a package when ctrlflow finishes there and, where Analyzer runs too,
// creates the package the readers walk as NewPackage creates it, unbuilt, as the resolver's is when no question of its
// own reached it first. It keeps each package ReadersPackage handed out, so a test can tell run took the shared path.
type sharingResolver struct {
	mutex   sync.Mutex
	created map[*types.Package]*intermediate_representation.Package
	handed  map[*types.Package]*intermediate_representation.Package
}

// newSharingResolver is a resolver that has learned no package yet.
func newSharingResolver() *sharingResolver {
	return &sharingResolver{
		created: map[*types.Package]*intermediate_representation.Package{},
		handed:  map[*types.Package]*intermediate_representation.Package{},
	}
}

// Finished creates action's package for the readers when action is ctrlflow's and Analyzer runs on the package, with
// the expression map its readers ask for, as the resolver's Finished learns it.
func (resolver *sharingResolver) Finished(action *checker.Action) {
	graphs, isGraphs := action.Result.(*ctrlflow.CFGs)
	if action.Analyzer != ctrlflow.Analyzer || !isGraphs {
		return
	}
	for _, sibling := range action.PackageActions() {
		if sibling.Analyzer != Analyzer {
			continue
		}
		created := NewPackage(action.Package.Fset, action.Package.Types, action.Package.Syntax,
			action.Package.TypesInfo, graphs.NoReturn, ReadsExpressions(sibling.Dependents()))
		resolver.mutex.Lock()
		resolver.created[action.Package.Types] = created
		resolver.mutex.Unlock()
	}
}

// ReadersPackage is the package Finished created for packageTypes, kept as handed out, or nil where it created none.
func (resolver *sharingResolver) ReadersPackage(packageTypes *types.Package) *intermediate_representation.Package {
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	created := resolver.created[packageTypes]
	if created != nil {
		resolver.handed[packageTypes] = created
	}
	return created
}

// handedOut is the package ReadersPackage handed out for packageTypes, or nil when it was never asked or had none.
func (resolver *sharingResolver) handedOut(packageTypes *types.Package) *intermediate_representation.Package {
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	return resolver.handed[packageTypes]
}

// withAnonymous is functions and every anonymous function each holds, however deep.
func withAnonymous(functions []*intermediate_representation.Function) []*intermediate_representation.Function {
	every := []*intermediate_representation.Function{}
	for _, function := range functions {
		every = append(every, function)
		every = append(every, withAnonymous(function.AnonymousFunctions)...)
	}
	return every
}

// A reader that follows StaticCallee from a function into one of the same package declared after it finds the callee
// built, as staticcheck's SA2002 needs, though the walk hasn't reached the callee yet. The fixture's caller is
// declared before its callee, and the probe reads the callee's blocks at the call, in the walk's order.
func TestALaterCalleeIsBuiltWhenItsCallerIsRead(test *testing.T) {
	test.Parallel()
	probe := &analysis.Analyzer{
		Name:       "callee_probe",
		Doc:        "names each same-package callee and whether it had blocks when its call was read",
		Requires:   []*analysis.Analyzer{Analyzer},
		ResultType: reflect.TypeFor[[]string](),
		Run: func(pass *analysis.Pass) (any, error) {
			reached := []string{}
			for function := range mustResultOf(pass).SourceFunctions() {
				if function.Name() != "caller" {
					continue
				}
				for _, block := range function.Blocks {
					for _, instruction := range block.Instructions {
						call, isCall := instruction.(*intermediate_representation.Call)
						if !isCall {
							continue
						}
						if callee := call.Call.StaticCallee(); callee != nil && callee.Package == function.Package {
							reached = append(reached, fmt.Sprintf("%s built %t", callee.Name(), len(callee.Blocks) > 0))
						}
					}
				}
			}
			return reached, nil
		},
	}
	reached, isReached := rootResults(test, &checker.Options{}, probe)[probe].([]string)
	if !isReached || !slices.Equal(reached, []string{"callee built true"}) {
		test.Errorf("the caller reached %q, where its later callee, built, was expected", reached)
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
	for function := range result.SourceFunctions() {
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
