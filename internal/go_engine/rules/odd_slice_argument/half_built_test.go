package odd_slice_argument_test

import (
	"fmt"
	"go/ast"
	"go/token"
	"hash/crc32"
	"maps"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// initializer names a package's synthetic initializer among its functions, which has no object to name it by.
const initializer = "init"

// For half of a package's functions, with nothing else built, the rule finds in them what a run over the whole package
// finds there (#8d9r2rh): over the fixtures, the rule's findings in a function are a function of that function's own
// instructions and of the marks it asks, and a mark is judged from only what its question reaches. The half is
// seeded by the package's path, so it is the same half every run.
func TestHalfAPackagesFunctionsFindWhatTheWholeRunFindsThere(test *testing.T) {
	test.Parallel()
	patterns := []string{"CheckEvenSliceLength", "parity", "differences", "statements", "order/forward", "order/back",
		"standin/first", "standin/last", "instance/first", "instance/last", "around"}
	requireHalvesFindAsTheWhole(test, analysistest.TestData(), patterns)
}

// Over the generated graphs, each with a probe for every function, half the functions find what the whole run finds
// in them, and the whole run finds at each probe exactly what the graph it was generated from marks.
func TestHalfAGeneratedPackagesFunctionsFindWhatTheWholeRunFindsThere(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	variants := writeGeneratedVariants(test, directory, rand.New(rand.NewPCG(generatedSeed, 0)))
	var patterns []string
	for _, variant := range variants {
		patterns = append(patterns, variant.path)
	}
	whole := requireHalvesFindAsTheWhole(test, directory, patterns)
	for _, variant := range variants {
		marks := variant.graph.marks()
		for index := range variant.graph.functions {
			var want []string
			for parameter, name := range generatedParameters {
				if marks[index][parameter] {
					want = append(want, fmt.Sprintf("argument %q is expected to have even number of elements, but "+
						"has 1 elements", name))
				}
			}
			probe := fmt.Sprintf("%s.Probe%d", variant.path, index)
			if found := whole[variant.path].messages[probe]; !slices.Equal(found, want) {
				test.Errorf("%s: the whole run finds %q, and the graph marks %q", probe, found, want)
			}
		}
	}
}

// wholeRun is what a run of the rule over one whole package found: its findings by the function they are in, the
// function's full name or the initializer for one in no function, each as its file's name, place and message, the
// same findings' messages alone, and the declared functions each function's body, and the literals in it, call.
type wholeRun struct {
	findings map[string][]string
	messages map[string][]string
	calls    map[string][]string
}

// halfRun is what a run over half a package's functions left: the functions it judged, by name, and which of the
// package's functions were built when it ended.
type halfRun struct {
	chosen []string
	built  map[string]bool
}

// requireHalvesFindAsTheWhole runs the rule over each package patterns name in directory twice, loaded apart: over the
// whole package, as the engine runs it, and over a seeded half of its functions with nothing else built, and requires
// the half's findings to be the whole run's in those functions. It requires that the half built nothing unrelated,
// which is every function but the ones judged and the ones a static call leads to from them, since a question's
// judgment reaches only along such calls, and that some function somewhere was left so. It returns each package's
// whole run, by path.
func requireHalvesFindAsTheWhole(test *testing.T, directory string, patterns []string) map[string]wholeRun {
	test.Helper()
	calls := &analysis.Analyzer{
		Name:       "declared_calls",
		Doc:        "the declared functions each function of the package calls, from the package built whole",
		Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer},
		ResultType: reflect.TypeFor[map[string][]string](),
		Run:        declaredCalls,
	}
	wholes := map[string]wholeRun{}
	wholeGraph, wholeError := checker.Analyze([]*analysis.Analyzer{odd_slice_argument.Analyzer, calls},
		loadFixtures(test, directory, patterns), check.DispatchOptions())
	if wholeError != nil {
		test.Fatalf("analyzing %v whole: %v", patterns, wholeError)
	}
	for _, action := range wholeGraph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s on %s: %v", action.Analyzer.Name, action.Package.ID, action.Err)
		}
		whole := wholes[action.Package.PkgPath]
		if called, isCalls := action.Result.(map[string][]string); isCalls {
			whole.calls = called
		}
		if action.Analyzer == odd_slice_argument.Analyzer {
			whole.findings, whole.messages = findingsByFunction(action)
		}
		wholes[action.Package.PkgPath] = whole
	}
	half := &analysis.Analyzer{
		Name: "half_built",
		Doc:  "the rule over a seeded half of the package's functions, with nothing else built",
		// No build of the package's representation is required, so nothing is built but what this run builds.
		Requires:   []*analysis.Analyzer{odd_slice_argument.MarksAnalyzer, generated.Analyzer},
		ResultType: reflect.TypeFor[halfRun](),
		Run:        runHalf,
	}
	halfGraph, halfError := checker.Analyze([]*analysis.Analyzer{half}, loadFixtures(test, directory, patterns),
		check.DispatchOptions())
	if halfError != nil {
		test.Fatalf("analyzing half of %v: %v", patterns, halfError)
	}
	found := 0
	unrelated := 0
	for _, action := range halfGraph.Roots {
		ran, isRan := action.Result.(halfRun)
		if action.Err != nil || !isRan {
			test.Fatalf("running on half of %s: %v", action.Package.ID, action.Err)
		}
		whole := wholes[action.Package.PkgPath]
		halfFindings, _ := findingsByFunction(action)
		for _, name := range slices.Sorted(maps.Keys(halfFindings)) {
			if !slices.Contains(ran.chosen, name) {
				test.Errorf("%s: the half run finds %q in %s, which it never judged", action.Package.ID,
					halfFindings[name], name)
			}
		}
		for _, name := range ran.chosen {
			found += len(halfFindings[name])
			if !slices.Equal(halfFindings[name], whole.findings[name]) {
				test.Errorf("%s, %s: judged with nothing else built it finds %q, and the whole run finds %q there",
					action.Package.ID, name, halfFindings[name], whole.findings[name])
			}
		}
		related := map[string]bool{}
		var reach func(name string)
		reach = func(name string) {
			if related[name] {
				return
			}
			related[name] = true
			for _, callee := range whole.calls[name] {
				reach(callee)
			}
		}
		for _, name := range ran.chosen {
			reach(name)
		}
		for _, name := range slices.Sorted(maps.Keys(ran.built)) {
			if related[name] {
				continue
			}
			unrelated++
			if ran.built[name] {
				test.Errorf("%s: the half run built %s, which it did not judge and no call leads to from what it "+
					"judged", action.Package.ID, name)
			}
		}
	}
	if found == 0 {
		test.Fatalf("the half runs over %v found nothing, so the comparison proves nothing", patterns)
	}
	if unrelated == 0 {
		test.Fatalf("no function of %v was unrelated to its package's half, so nothing shows a function left unbuilt",
			patterns)
	}
	test.Logf("over %d packages the halves found %d findings, the whole run's there, and left %d unrelated functions "+
		"unbuilt", len(halfGraph.Roots), found, unrelated)
	return wholes
}

// runHalf judges a seeded half of pass's package's functions through the rule's own Check, in the package as the
// resolver creates it, where nothing is built until this run or a question of its builds it, and reports what Check
// finds.
func runHalf(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	marks, isMarks := pass.ResultOf[odd_slice_argument.MarksAnalyzer].(*odd_slice_argument.Result)
	if !isMarks {
		return nil, fmt.Errorf("the marks' result is a %T", pass.ResultOf[odd_slice_argument.MarksAnalyzer])
	}
	resolver, resolverError := fact_resolver.Of(pass)
	if resolverError != nil {
		return nil, resolverError
	}
	home := resolver.Package(pass.Pkg)
	if home == nil {
		return nil, fmt.Errorf("the resolver never learned %s", pass.Pkg.Path())
	}
	functions := map[string]*intermediate_representation.Function{}
	for _, function := range home.IntermediateRepresentation().Functions {
		if function.Blocks != nil {
			return nil, fmt.Errorf("%s was built before the half run began", functionName(function))
		}
		functions[functionName(function)] = function
	}
	names := slices.Sorted(maps.Keys(functions))
	shuffle := rand.New(rand.NewPCG(generatedSeed, uint64(crc32.ChecksumIEEE([]byte(pass.Pkg.Path())))))
	shuffle.Shuffle(len(names), func(left, right int) { names[left], names[right] = names[right], names[left] })
	ran := halfRun{chosen: slices.Sorted(slices.Values(names[:(len(names)+1)/2])), built: map[string]bool{}}
	var judge func(function *intermediate_representation.Function)
	judge = func(function *intermediate_representation.Function) {
		odd_slice_argument.Check(context, marks, function)
		for _, anonymous := range function.AnonymousFunctions {
			judge(anonymous)
		}
	}
	for _, name := range ran.chosen {
		resolver.Build(functions[name])
		judge(functions[name])
	}
	for name, function := range functions {
		ran.built[name] = function.Blocks != nil
	}
	return ran, nil
}

// declaredCalls is, for each function pass's package declares and its initializer, by name, the package's declared
// functions that its body and the function literals in it call statically, each named as the function it stands for
// when the callee is an instantiation or a wrapper of one.
func declaredCalls(pass *analysis.Pass) (any, error) {
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	calls := map[string][]string{}
	var read func(name string, function *intermediate_representation.Function)
	read = func(name string, function *intermediate_representation.Function) {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instructions {
				call, isCall := instruction.(intermediate_representation.CallInstruction)
				if !isCall {
					continue
				}
				callee := call.Common().StaticCallee()
				if callee == nil {
					continue
				}
				for callee.Parent() != nil {
					callee = callee.Parent()
				}
				if declared, isDeclared := callee.Object().(*types.Func); isDeclared && declared.Pkg() == pass.Pkg {
					calls[name] = append(calls[name], declared.Origin().FullName())
				} else if callee.Package == built.Package() {
					calls[name] = append(calls[name], functionName(callee))
				}
			}
		}
		for _, anonymous := range function.AnonymousFunctions {
			read(name, anonymous)
		}
	}
	for _, function := range built.Package().Functions {
		calls[functionName(function)] = nil
		read(functionName(function), function)
	}
	return calls, nil
}

// functionName is a declared function's full name, as go/types prints it, or the initializer's.
func functionName(function *intermediate_representation.Function) string {
	if declared, isDeclared := function.Object().(*types.Func); isDeclared {
		return declared.FullName()
	}
	return initializer
}

// findingsByFunction is action's findings by the function each is in, the full name of the function declaration
// around it or the initializer for one in none, each as its file's name, its place and its message, sorted, and the
// same findings' messages alone.
func findingsByFunction(action *checker.Action) (findings map[string][]string, messages map[string][]string) {
	findings, messages = map[string][]string{}, map[string][]string{}
	for _, diagnostic := range action.Diagnostics {
		name := enclosingFunction(action.Package, diagnostic.Pos)
		position := action.Package.Fset.Position(diagnostic.Pos)
		findings[name] = append(findings[name], fmt.Sprintf("%s:%d:%d: %s", filepath.Base(position.Filename),
			position.Line, position.Column, diagnostic.Message))
		messages[name] = append(messages[name], diagnostic.Message)
	}
	for name := range findings {
		slices.Sort(findings[name])
		slices.Sort(messages[name])
	}
	return findings, messages
}

// enclosingFunction is the full name of the function declaration of loaded that position is in, or the initializer for
// a position in none, as one in a package-level variable's value.
func enclosingFunction(loaded *packages.Package, position token.Pos) string {
	for _, file := range loaded.Syntax {
		if position < file.FileStart || position > file.FileEnd {
			continue
		}
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || position < function.Pos() || position > function.End() {
				continue
			}
			if declared, isDeclared := loaded.TypesInfo.Defs[function.Name].(*types.Func); isDeclared {
				return declared.FullName()
			}
		}
	}
	return initializer
}
