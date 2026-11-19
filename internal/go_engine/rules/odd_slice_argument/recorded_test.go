package odd_slice_argument_test

import (
	"go/types"
	"maps"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
)

// hearingHand is a test's hand on a run's answers that answers nothing itself and hears every answer the run computes
// of the marks, each by its object, as many times as it was told.
type hearingHand struct {
	lock  sync.Mutex
	heard map[types.Object][]string
}

func (*hearingHand) Answer(string, types.Object) ([]byte, bool) {
	return nil, false
}

func (hand *hearingHand) Computed(kindName string, object types.Object, encoded []byte) {
	if kindName != odd_slice_argument.Kind.Name {
		return
	}
	hand.lock.Lock()
	defer hand.lock.Unlock()
	hand.heard[object] = append(hand.heard[object], string(encoded))
}

// Every mark a judgment settles reaches the resolver's record as an asked one does (#8d9r2rh). Over order/forward the
// rule asks one mark, of back's slice, for odd's call. Judging it settles forward's and pairs' too, which back's
// answer rests on and nobody asks. An answer on record is a sound place to stop comparing two builds only when
// everything it read is on record beside it, so the resolver tells all three as it tells an answer it was asked, each
// once and marked, and nothing else: enter and odd take no slice.
func TestEveryMarkAJudgmentSettlesIsRecorded(test *testing.T) {
	test.Parallel()
	roots := loadFixtures(test, analysistest.TestData(), []string{"order/forward"})
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	hand := &hearingHand{heard: map[types.Object][]string{}}
	resolver.HoldAnswers(hand)
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{odd_slice_argument.Analyzer}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing order/forward: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running on %s: %v", action.Package.ID, action.Err)
		}
	}
	heard := map[string][]string{}
	for object, answers := range hand.heard {
		heard[object.Name()+" at "+roots[0].Fset.Position(object.Pos()).String()] = answers
	}
	want := map[string][]string{}
	for _, name := range []string{"back", "forward", "pairs"} {
		function, isFunction := roots[0].Types.Scope().Lookup(name).(*types.Func)
		if !isFunction {
			test.Fatalf("order/forward declares no function %s", name)
		}
		parameter := function.Signature().Params().At(0)
		want[parameter.Name()+" at "+roots[0].Fset.Position(parameter.Pos()).String()] = []string{"true"}
	}
	if !maps.EqualFunc(heard, want, slices.Equal) {
		test.Errorf("the run told the marks %v, want each of %v told once, marked", heard,
			slices.Sorted(maps.Keys(want)))
	}
}
