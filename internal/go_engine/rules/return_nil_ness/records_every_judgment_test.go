package return_nil_ness_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// ownFunctions is every function and method own declares, sorted by full name.
func ownFunctions(own *types.Package) []*types.Func {
	var functions []*types.Func
	scope := own.Scope()
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
	slices.SortFunc(functions, func(left *types.Func, right *types.Func) int {
		return cmp.Compare(left.FullName(), right.FullName())
	})
	return functions
}

// The first half of what the kind's flag claims (fact_resolver.Kind.RecordsEveryJudgment): every verdict nilness's own
// state holds of a function with a result that may be nil is an answer the resolver holds, the same one. Over the
// package's fixtures, a run asks every other function of each package that has a result, by name, and a second run the
// rest, so each walk judges callees nobody asked of, the demand walk's and the replay's both: after each, every such
// verdict in the kind's state is among the resolver's answers (fact_resolver.Answered), and some of them were never
// asked of, which only a verdict handed over (fact_resolver.Keep) explains.
func TestEveryVerdictNilnessHoldsIsAnAnswerTheResolverHolds(test *testing.T) {
	test.Parallel()
	if !return_nil_ness.Kind.RecordsEveryJudgment {
		test.Fatal("the kind does not say it records every judgment, so there is nothing to hold it to")
	}
	directory := analysistest.TestData()
	patterns := []string{"assembled", "differences", "kept", "nilness", "ordered/...", "replayed/...", "unbuilt"}
	held, unasked, replays := 0, 0, int64(0)
	for parity := range 2 {
		loaded, loadError := load.Packages(directory, patterns, []string{
			"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
		}, nil)
		if loadError != nil {
			test.Fatalf("loading %v: %v", patterns, loadError)
		}
		probe := &analysis.Analyzer{
			Name:       "every_other_function",
			Doc:        "asks the nilness of every other function of the package that has a result",
			Requires:   []*analysis.Analyzer{return_nil_ness.Analyzer},
			ResultType: reflect.TypeFor[[]*types.Func](),
			Run: func(pass *analysis.Pass) (any, error) {
				nilness, isNilness := pass.ResultOf[return_nil_ness.Analyzer].(*return_nil_ness.Result)
				if !isNilness {
					return nil, fmt.Errorf("return_nil_ness's result is a %T", pass.ResultOf[return_nil_ness.Analyzer])
				}
				var asked []*types.Func
				for index, function := range ownFunctions(pass.Pkg) {
					if index%2 == parity && function.Signature().Results().Len() > 0 {
						nilness.Nilness(function, 0)
						asked = append(asked, function)
					}
				}
				return asked, nil
			},
		}
		options := check.DispatchOptions()
		resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
		if !isResolver {
			test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
		}
		graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loaded.Roots, options)
		if analyzeError != nil {
			test.Fatalf("analyzing %v: %v", patterns, analyzeError)
		}
		answered := map[types.Object]string{}
		for _, answer := range resolver.Answered() {
			if answer.Kind == return_nil_ness.Kind {
				answered[answer.Object] = string(answer.Encoded)
			}
		}
		for _, action := range graph.Roots {
			asked, isAsked := action.Result.([]*types.Func)
			if action.Err != nil || !isAsked {
				test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
			}
			home := resolver.Package(action.Package.Types)
			if home == nil {
				test.Fatalf("the resolver never learned %s", action.Package.ID)
			}
			for function, results := range return_nil_ness.HeldVerdicts(home) {
				held++
				if !slices.Contains(asked, function) {
					unasked++
				}
				want, encodeError := json.Marshal(results)
				if encodeError != nil {
					test.Fatal(encodeError)
				}
				if answer, isAnswered := answered[function]; !isAnswered || answer != string(want) {
					test.Errorf("%s: the kind holds the verdict %s, and the resolver the answer %q (held: %t)",
						function.FullName(), want, answer, isAnswered)
				}
			}
		}
		replays += resolver.Counts(return_nil_ness.Kind).Fallbacks.Load()
	}
	if held == 0 || unasked == 0 || replays == 0 {
		test.Errorf("the kind held %d verdicts, %d of them of functions nobody asked of, over %d replays, want some "+
			"of each, or the pin holds nothing of one of the walks", held, unasked, replays)
	}
}
