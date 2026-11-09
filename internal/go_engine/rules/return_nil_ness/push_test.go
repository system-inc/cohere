package return_nil_ness

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// pushResult is the nilness of every function's results the push's facts hold in a package, its own and its
// imports', as the analyzer's Result held them before it was asked on demand.
type pushResult struct {
	functions map[*types.Func][]ValueNilness
}

// newPushAnalyzer is the analyzer as it was before it answered on demand (#w7zmvat unit 2), kept for the tests as the
// original the resolver answers as: it judges every source function of every package, dependencies included, in
// buildir's order, exporting ReturnedNilness on each whose results say more than the defaults. judged, when not nil,
// counts the functions it judged over a run, the push's total that the resolver's computed is held against.
func newPushAnalyzer(judged *atomic.Int64) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "return_nil_ness_push",
		Doc:  "annotates return values with their nilness, every function of every package, as the facts were pushed",
		Run: func(pass *analysis.Pass) (any, error) {
			return runPush(pass, judged)
		},
		Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer},
		FactTypes:  []analysis.Fact{new(ReturnedNilness)},
		ResultType: reflect.TypeFor[*pushResult](),
	}
}

// pushAnalyzer is the push for the tests that read only its facts.
var pushAnalyzer = newPushAnalyzer(nil)

// runPush judges every source function of the package, in buildir's order, and returns every function's results the
// facts hold.
func runPush(pass *analysis.Pass, judged *atomic.Int64) (*pushResult, error) {
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	functions := slices.Collect(built.SourceFunctionsReadBy(pass))
	walk := &pushWalk{facts: passFacts{pass: pass}, irPackage: built.Package(),
		seen: map[*intermediate_representation.Function]bool{}}
	for _, function := range functions {
		walk.returns(function)
	}
	if judged != nil {
		judged.Add(int64(len(walk.seen)))
	}
	result := &pushResult{functions: map[*types.Func][]ValueNilness{}}
	for _, fact := range pass.AllObjectFacts() {
		function, isFunction := fact.Object.(*types.Func)
		returned, isReturned := fact.Fact.(*ReturnedNilness)
		if !isFunction || !isReturned {
			return nil, fmt.Errorf("reading return_nil_ness's facts: %s holds a %T, where only a function's "+
				"ReturnedNilness was expected", fact.Object, fact.Fact)
		}
		result.functions[function] = returned.Results
	}
	return result, nil
}

// Nilness is the nilness of the result at index result of function as the push's facts hold it, as the analyzer's
// Result answered it before it was asked on demand.
func (pushed *pushResult) Nilness(function *types.Func, result int) ValueNilness {
	resultType := function.Signature().Results().At(result).Type()
	if !typeutil.MaybePointerLike(resultType) {
		return ValueNilness{Outer: NeverNil}
	}
	if len(pushed.functions[function]) == 0 {
		return ValueNilness{Inner: MaybeNil, Outer: MaybeNil}
	}
	return normalize(pushed.functions[function][result], resultType)
}
