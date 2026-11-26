package parity

import (
	"fmt"
	"go/types"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	original_printf "golang.org/x/tools/go/analysis/passes/printf"
	"honnef.co/go/tools/analysis/facts/nilness"
	"honnef.co/go/tools/analysis/facts/purity"
	"honnef.co/go/tools/go/types/typeutil"
	"honnef.co/go/tools/staticcheck/sa5012"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/project"
	"github.com/system-inc/cohere/internal/go_language/rules/function_purity"
	"github.com/system-inc/cohere/internal/go_language/rules/odd_slice_argument"
	"github.com/system-inc/cohere/internal/go_language/rules/return_nil_ness"
	"github.com/system-inc/cohere/internal/go_language/rules/vet/printf"
)

// ownFactKinds are the kinds the own-facts differential compares, each of which the original must hold on an
// unexported function of the module, so none of them passes on nothing.
var ownFactKinds = []string{"purity", "nil-ness", "odd slice", "printf wrapper"}

// Over module_own_facts, every function's facts are cohere's as the originals hold them, the unexported functions'
// among them, which no importer inherits and RequireIdenticalFacts never compares: purity, the nil-ness of each
// result and the odd slice mark of each parameter as cohere answers them on demand (function_purity, return_nil_ness
// and odd_slice_argument's results) against staticcheck's pushed facts, and printf wrappers as the port exports them
// against x/tools' printf. Each answer is keyed by its object's objectpath or, for an object with none, its position,
// and the test fails on each that differs. The module holds no-return and deprecation facts nowhere, since cohere has
// no port of either, running x/tools' ctrlflow and staticcheck's deprecated analyzer themselves, and so no oracle to
// compare them with.
func TestOwnFactsMatchTheOriginalsOnUnexportedFunctions(test *testing.T) {
	test.Parallel()
	answers := ownFactsOf(test, filepath.Join(analysistest.TestData(), "module_own_facts"))
	for _, kind := range ownFactKinds {
		test.Logf("%s: %d answers compared, %d of them a fact the original holds on an unexported function", kind,
			answers.compared[kind], answers.held[kind])
		if answers.held[kind] == 0 {
			test.Errorf("the original holds no %s fact on an unexported function, so its comparison proves nothing "+
				"there", kind)
		}
	}
	keys := slices.Concat(slices.Collect(maps.Keys(answers.original)), slices.Collect(maps.Keys(answers.ours)))
	slices.Sort(keys)
	for _, key := range slices.Compact(keys) {
		original, originalAnswered := answers.original[key]
		ours, oursAnswered := answers.ours[key]
		if !originalAnswered || !oursAnswered || original != ours {
			test.Errorf("%s (%s): the original answers %q, cohere %q", answers.names[key], key, original, ours)
		}
	}
}

// ownFactsOf loads the module at directory as cohere-go does and answers, for every function of its one package and
// every result and parameter of each, each kind of ownFactKinds both ways: cohere's demand kinds asked of their
// results and the original's read from staticcheck's, by a probe beside them, and odd slice marks and printf wrappers
// read from the facts each side exports on the package's own objects once the run is done.
func ownFactsOf(test *testing.T, directory string) ownFactAnswers {
	test.Helper()
	defer Target{}.held(test)()
	workspace, workspaceError := project.ReadWorkspace(directory)
	if workspaceError != nil {
		test.Fatalf("reading the module of %s: %v", directory, workspaceError)
	}
	loaded, loadError := load.Packages(workspace.Root, []string{"./..."}, workspace.Environment(), nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", workspace.Root, loadError)
	}
	if len(loaded.Packages) != 1 {
		test.Fatalf("loading %s found %d packages, want the module's one", workspace.Root, len(loaded.Packages))
	}
	probe := &analysis.Analyzer{
		Name: "own_facts_probe",
		Doc:  "asks cohere's demand results and staticcheck's of every function, result and parameter its package declares",
		Requires: []*analysis.Analyzer{purity.Analyzer, function_purity.Analyzer, nilness.Analysis,
			return_nil_ness.Analyzer, odd_slice_argument.MarksAnalyzer},
		ResultType: reflect.TypeFor[ownFactAnswers](),
		Run:        runOwnFactsProbe,
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe, sa5012.Analyzer, original_printf.Analyzer,
		printf.Analyzer}, loaded.Packages, portOptions())
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", workspace.Root, analyzeError)
	}
	var answers ownFactAnswers
	marks := map[types.Object]string{}
	originalWrappers, ourWrappers := map[types.Object]string{}, map[types.Object]string{}
	for action := range graph.All() {
		if action.Err != nil {
			test.Fatalf("running %s on %s: %v", action.Analyzer.Name, action.Package.ID, action.Err)
		}
		switch action.Analyzer {
		case probe:
			found, isFound := action.Result.(ownFactAnswers)
			if !isFound {
				test.Fatalf("the probe's result on %s is a %T", action.Package.ID, action.Result)
			}
			answers = found
		case sa5012.Analyzer:
			maps.Copy(marks, ownFactsOn(action))
		case original_printf.Analyzer:
			maps.Copy(originalWrappers, ownFactsOn(action))
		case printf.Analyzer:
			maps.Copy(ourWrappers, ownFactsOn(action))
		}
	}
	if answers.original == nil {
		test.Fatalf("the probe never ran on %s", workspace.Root)
	}
	module := loaded.Packages[0]
	for _, function := range ownFunctions(module.Types) {
		recordOwnFact(answers, module.Fset, "printf wrapper", function, function, "",
			factOrNone(originalWrappers, function), factOrNone(ourWrappers, function), originalWrappers[function] != "")
		for parameter := range function.Signature().Params().Variables() {
			ours, asked := answers.marked[parameter]
			recordOwnFact(answers, module.Fset, "odd slice", parameter, function, " parameter "+parameter.Name(),
				fmt.Sprint(marks[parameter] != ""), answerOrNotAsked(asked, ours), marks[parameter] != "")
		}
	}
	return answers
}

// runOwnFactsProbe answers purity and the nil-ness of each result of every function of pass's package both ways, from
// cohere's results and staticcheck's, and asks cohere's odd slice mark of each parameter, whose original only the
// original's facts hold.
func runOwnFactsProbe(pass *analysis.Pass) (any, error) {
	originalPurity, isOriginalPurity := pass.ResultOf[purity.Analyzer].(purity.Result)
	ourPurity, isOurPurity := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
	originalNilness, isOriginalNilness := pass.ResultOf[nilness.Analysis].(*nilness.Result)
	ourNilness, isOurNilness := pass.ResultOf[return_nil_ness.Analyzer].(*return_nil_ness.Result)
	ourMarks, isOurMarks := pass.ResultOf[odd_slice_argument.MarksAnalyzer].(*odd_slice_argument.Result)
	if !isOriginalPurity || !isOurPurity || !isOriginalNilness || !isOurNilness || !isOurMarks {
		return nil, fmt.Errorf("the probe's required results are not their analyzers' types on %s", pass.Pkg.Path())
	}
	found := newOwnFactAnswers()
	for _, function := range ownFunctions(pass.Pkg) {
		originalPure := originalPurity[function] != nil
		recordOwnFact(found, pass.Fset, "purity", function, function, "", fmt.Sprint(originalPure),
			fmt.Sprint(ourPurity.IsPure(function)), originalPure)
		results := function.Signature().Results()
		for index := range results.Len() {
			original := originalNilness.Nilness(function, index)
			// A result that cannot be nil reads never nil from its type alone, which is no fact either side holds.
			holds := typeutil.MaybePointerLike(results.At(index).Type()) && original.Outer == nilness.NeverNil
			recordOwnFact(found, pass.Fset, "nil-ness", function, function, fmt.Sprintf(" result %d", index),
				fmt.Sprint(original), fmt.Sprint(ourNilness.Nilness(function, index)), holds)
		}
		for parameter := range function.Signature().Params().Variables() {
			found.marked[parameter] = ourMarks.Marked(parameter)
		}
	}
	return found, nil
}

// ownFunctions is every function and method packageTypes declares at package level, in the order of their names.
func ownFunctions(packageTypes *types.Package) []*types.Func {
	var functions []*types.Func
	scope := packageTypes.Scope()
	for _, name := range scope.Names() {
		switch object := scope.Lookup(name).(type) {
		case *types.Func:
			functions = append(functions, object)
		case *types.TypeName:
			if named, isNamed := object.Type().(*types.Named); isNamed {
				functions = append(functions, slices.Collect(named.Methods())...)
			}
		}
	}
	return functions
}

// ownFactsOn is every fact action exports on its own package's objects, as `%v` prints it, by its object.
// AllObjectFacts holds what the action inherited from the packages below too, so only the facts on its own package's
// objects are its own.
func ownFactsOn(action *checker.Action) map[types.Object]string {
	own := map[types.Object]string{}
	for _, objectFact := range action.AllObjectFacts() {
		if objectFact.Object.Pkg() == action.Package.Types {
			own[objectFact.Object] = fmt.Sprint(objectFact.Fact)
		}
	}
	return own
}

// factOrNone is the fact facts hold of object, or that they hold none.
func factOrNone(facts map[types.Object]string, object types.Object) string {
	if fact, holds := facts[object]; holds {
		return fact
	}
	return "no fact"
}

// answerOrNotAsked is cohere's answer about a parameter, or that the probe never asked it.
func answerOrNotAsked(asked bool, answer bool) string {
	if asked {
		return fmt.Sprint(answer)
	}
	return "not asked"
}
