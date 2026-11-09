package odd_slice_argument

import (
	"go/types"
	"reflect"
	"slices"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// pushResult is every parameter the push's marks hold in a package, its own and its imports'.
type pushResult map[*types.Var]bool

// newPushAnalyzer is the analyzer as it was before it asked the resolver (#w7zmvat unit 2), kept for the tests as the
// original the resolver answers as: it marks every source function's parameters on every package, dependencies
// included, exporting evenElements on each, reports as the analyzer does, and its result is every parameter the marks
// hold. judged, when not nil, counts the source functions it marked over a run, the push's total that the resolver's
// computed is held against.
func newPushAnalyzer(judged *atomic.Int64) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:       "SA5012",
		Doc:        "Flags a slice of odd length passed to a function that needs an even one, its marks pushed.",
		Requires:   []*analysis.Analyzer{build_intermediate_representation.Analyzer, generated.Analyzer},
		FactTypes:  []analysis.Fact{new(evenElements)},
		ResultType: reflect.TypeFor[pushResult](),
		Run: func(pass *analysis.Pass) (any, error) {
			return runPush(pass, judged)
		},
	}
}

// pushAnalyzer is the push for the tests that read only its marks and findings.
var pushAnalyzer = newPushAnalyzer(nil)

// runPush marks the package's parameters the push's way, reports the odd slices passed to marked ones, and returns
// every parameter the marks hold.
func runPush(pass *analysis.Pass, judged *atomic.Int64) (pushResult, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	built, readError := build_intermediate_representation.ResultOf(pass)
	if readError != nil {
		return nil, readError
	}
	functions := slices.Collect(built.SourceFunctions())
	marks := passFacts{pass: pass}
	walk := &pushWalk{facts: marks}
	walk.markLengthChecks(functions)
	walk.markIndirectLengthChecks(functions)
	reportOddLengths(context, marks, functions)
	if judged != nil {
		judged.Add(int64(len(functions)))
	}
	result := pushResult{}
	for _, fact := range pass.AllObjectFacts() {
		if parameter, isParameter := fact.Object.(*types.Var); isParameter {
			result[parameter] = true
		}
	}
	return result, nil
}
