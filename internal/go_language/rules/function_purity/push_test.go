package function_purity

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/rules/build_intermediate_representation"
)

// pushResult is every function the push's facts hold pure in a package, its own and its imports', as staticcheck's
// purity.Result holds them, and as the analyzer's Result held them before it was asked on demand.
type pushResult map[*types.Func]*IsPure

// newPushAnalyzer is the analyzer as it was before it answered on demand (#w7zmvat unit 2), kept for the tests as the
// original the resolver answers as: it judges every source function of every package, dependencies included, in
// buildir's order, exporting IsPure on each pure one, and its result is every function the facts hold pure.
func newPushAnalyzer() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "function_purity_push",
		Doc:  "marks the functions that are pure, every function of every package, as the facts were pushed",
		Run: func(pass *analysis.Pass) (any, error) {
			return runPush(pass)
		},
		Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer},
		FactTypes:  []analysis.Fact{new(IsPure)},
		ResultType: reflect.TypeFor[pushResult](),
	}
}

// pushAnalyzer is the push for the tests that read only its facts.
var pushAnalyzer = newPushAnalyzer()

// runPush judges every source function of the package, in buildir's order, and returns every function the facts hold
// pure.
func runPush(pass *analysis.Pass) (pushResult, error) {
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	functions := slices.Collect(built.SourceFunctions())
	walk := &pushWalk{facts: passFacts{pass: pass}, irPackage: built.Package(),
		seen: map[*intermediate_representation.Function]bool{}}
	for _, function := range functions {
		walk.isPure(function)
	}
	result := pushResult{}
	for _, fact := range pass.AllObjectFacts() {
		function, isFunction := fact.Object.(*types.Func)
		pure, isPure := fact.Fact.(*IsPure)
		if !isFunction || !isPure {
			return nil, fmt.Errorf("reading function_purity's facts: %s holds a %T, where only a function's IsPure "+
				"was expected", fact.Object, fact.Fact)
		}
		result[function] = pure
	}
	return result, nil
}
