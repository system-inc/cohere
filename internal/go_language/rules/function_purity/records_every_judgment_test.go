package function_purity_test

import (
	"cmp"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules/function_purity"
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

// The first half of what the kind's flag claims (fact_resolver.Kind.RecordsEveryJudgment): every verdict purity's own
// state holds is an answer the resolver holds, the same one. Over the package's fixtures, a run asks every other
// function of each package, by name, and a second run the rest, so each walk judges callees nobody asked of: after
// each, every verdict in the kind's state is among the resolver's answers (fact_resolver.Answered), and some of them
// were never asked of, which only a verdict handed over (fact_resolver.Keep) explains.
func TestEveryVerdictPurityHoldsIsAnAnswerTheResolverHolds(test *testing.T) {
	test.Parallel()
	if !function_purity.Kind.RecordsEveryJudgment {
		test.Fatal("the kind does not say it records every judgment, so there is nothing to hold it to")
	}
	directory := analysistest.TestData()
	patterns := []string{"purity", "kept", "demand/order/after", "demand/order/before", "demand/declares/first",
		"demand/declares/second", "demand/hops/top", "demand/hops/middle", "demand/hops/bottom"}
	held, unasked := 0, 0
	for parity := range 2 {
		loaded, loadError := load.Packages(directory, patterns, []string{
			"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
		}, nil)
		if loadError != nil {
			test.Fatalf("loading %v: %v", patterns, loadError)
		}
		probe := &analysis.Analyzer{
			Name:       "every_other_function",
			Doc:        "asks the purity of every other function of the package",
			Requires:   []*analysis.Analyzer{function_purity.Analyzer},
			ResultType: reflect.TypeFor[[]*types.Func](),
			Run: func(pass *analysis.Pass) (any, error) {
				purity, isPurity := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
				if !isPurity {
					return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
				}
				var asked []*types.Func
				for index, function := range ownFunctions(pass.Pkg) {
					if index%2 == parity {
						purity.IsPure(function)
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
			if answer.Kind == function_purity.Kind {
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
			for function, pure := range function_purity.Verdicts(home) {
				held++
				if !slices.Contains(asked, function) {
					unasked++
				}
				if answer, isAnswered := answered[function]; !isAnswered || answer != fmt.Sprint(pure) {
					test.Errorf("%s: the kind holds the verdict pure %t, and the resolver the answer %q (held: %t)",
						function.FullName(), pure, answer, isAnswered)
				}
			}
		}
	}
	if held == 0 || unasked == 0 {
		test.Errorf("the kind held %d verdicts, %d of them of functions nobody asked of, want some of each, or the "+
			"pin holds nothing", held, unasked)
	}
}
