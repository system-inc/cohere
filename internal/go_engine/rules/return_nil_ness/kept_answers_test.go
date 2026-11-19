package return_nil_ness_test

import (
	"encoding/json"
	"fmt"
	"go/types"
	"maps"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// keptAnswers is what one question left in the resolver of a fixture package: the resolver, the package as it holds
// it, every nilness answer it recorded for the package, decoded, nil for no fact, by its function's objectpath, and the
// kind's counts.
type keptAnswers struct {
	resolver *fact_resolver.Resolver
	home     *fact_resolver.Package
	recorded map[string][]return_nil_ness.ValueNilness
	counts   *fact_resolver.Counts
}

// askOnce runs a probe on the fixture package named fixture that asks the resolver the nilness of its function named
// asked, and of nothing else, and is what the resolver recorded for the package. The run has a store, which answers
// nothing, since only a run with a store records its answers (fact_resolver.Recorded), as every engine run with the
// findings cache does; the fixtures import nothing, so the store is asked of no package restored.
func askOnce(test *testing.T, fixture string, asked string) keptAnswers {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{fixture}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	probe := &analysis.Analyzer{
		Name:       "kept_answers_probe",
		Doc:        "asks the nilness of one function of the package and of nothing else",
		Requires:   []*analysis.Analyzer{return_nil_ness.Analyzer},
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			nilness, isNilness := pass.ResultOf[return_nil_ness.Analyzer].(*return_nil_ness.Result)
			if !isNilness {
				return nil, fmt.Errorf("return_nil_ness's result is a %T", pass.ResultOf[return_nil_ness.Analyzer])
			}
			function, isFunction := pass.Pkg.Scope().Lookup(asked).(*types.Func)
			if !isFunction {
				return nil, fmt.Errorf("%s declares no function %s", fixture, asked)
			}
			nilness.Nilness(function, 0)
			return "", nil
		},
	}
	resolver := fact_resolver.New(unansweringStore{})
	options := check.DispatchOptions()
	options.Resolver = resolver
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
		}
	}
	kept := keptAnswers{resolver: resolver, home: resolver.Package(loaded.Roots[0].Types),
		recorded: map[string][]return_nil_ness.ValueNilness{}, counts: resolver.Counts(return_nil_ness.Kind)}
	if kept.home == nil {
		test.Fatalf("the resolver never learned %s, which the probe ran on", fixture)
	}
	for path, encoded := range resolver.Recorded(loaded.Roots[0].Types)[return_nil_ness.Kind.Name] {
		var results []return_nil_ness.ValueNilness
		if decodeError := json.Unmarshal(encoded, &results); decodeError != nil {
			test.Fatalf("the answer recorded for %s, %s, does not decode: %v", path, encoded, decodeError)
		}
		kept.recorded[string(path)] = results
	}
	return kept
}

// requireRecorded fails unless the resolver recorded an answer for exactly the functions of want, each with want's
// results, nil for an answer of no fact.
func requireRecorded(test *testing.T, kept keptAnswers, want map[string][]return_nil_ness.ValueNilness) {
	test.Helper()
	if !maps.EqualFunc(kept.recorded, want, slices.Equal[[]return_nil_ness.ValueNilness]) {
		test.Errorf("the resolver recorded %v, want %v", kept.recorded, want)
	}
}

// Asked of Caller alone, the resolver holds an answer of its own for each callee Caller's judgment judged, none of
// which was asked of (#kemy8sq): Fresh never nil, with its results; Following, judged to no more than the defaults, as
// no fact; and Assembled, which has no body and takes the defaults unjudged, as no fact too. Caller's own answer, no
// fact, was built from those three, and a reader of the store's entry finds each beside it. No replay ran, so the
// answers are the ones the demand walk kept.
func TestEveryCalleeADemandWalkJudgesHasAnAnswerOfItsOwn(test *testing.T) {
	test.Parallel()
	kept := askOnce(test, "kept", "Caller")
	requireRecorded(test, kept, map[string][]return_nil_ness.ValueNilness{
		"Caller":    nil,
		"Fresh":     {{Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil}},
		"Following": nil,
		"Assembled": nil,
	})
	if fallbacks := kept.counts.Fallbacks.Load(); fallbacks != 0 {
		test.Errorf("asking of Caller replayed %d times, want 0, so the demand walk's answers went untested", fallbacks)
	}
}

// Asked of Inner alone, over the unbuilt fixture, the resolver replays the push's walk, which judges Outer on the way
// to Inner, and holds Outer's answer, never nil, beside Inner's, no fact, though no reader asked of Outer (#kemy8sq).
// Size and Count, which the replay answers the defaults and keeps no verdict of, have none.
func TestEveryFunctionAReplayJudgesHasAnAnswerOfItsOwn(test *testing.T) {
	test.Parallel()
	kept := askOnce(test, "unbuilt", "Inner")
	requireRecorded(test, kept, map[string][]return_nil_ness.ValueNilness{
		"Inner": nil,
		"Outer": {{Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil}},
	})
	if fallbacks := kept.counts.Fallbacks.Load(); fallbacks != 1 {
		test.Errorf("asking of Inner replayed %d times, want 1, so the replay's answers went untested", fallbacks)
	}
}

// What a replay keeps of a cycle is what a later question reads (#kemy8sq). CycleOuter and CycleInner call each other,
// and asked of CycleInner alone the resolver replays the push, which begins at CycleOuter: CycleInner, reading its
// begun partner as the defaults, may return nil, where judged first it would return what CycleOuter does, never nil.
// Each verdict was handed to the resolver as the walk kept it, CycleInner's while CycleOuter was still being judged,
// and each is final: of every function the resolver holds a record for, a question asked directly afterwards answers
// the record, and so does the kind's own verdict, which the record was made from and which no later judgment revised.
func TestWhatAReplayKeepsOfACycleIsWhatALaterQuestionReads(test *testing.T) {
	test.Parallel()
	kept := askOnce(test, "differences", "CycleInner")
	if fallbacks := kept.counts.Fallbacks.Load(); fallbacks != 1 {
		test.Fatalf("asking of CycleInner replayed %d times, want 1, so the replay's answers went untested", fallbacks)
	}
	for name, want := range map[string][]return_nil_ness.ValueNilness{
		"CycleInner": nil,
		"CycleOuter": {{Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil}},
	} {
		if record, isRecorded := kept.recorded[name]; !isRecorded || !slices.Equal(record, want) {
			test.Errorf("%s: the resolver recorded %v (recorded: %t), want %v", name, record, isRecorded, want)
		}
	}
	for path, record := range kept.recorded {
		object, objectError := objectpath.Object(kept.home.Types, objectpath.Path(path))
		function, isFunction := object.(*types.Func)
		if objectError != nil || !isFunction {
			test.Fatalf("the record at %s names %v, no function of the package: %v", path, object, objectError)
		}
		if asked, _ := return_nil_ness.Ask(kept.resolver, kept.home, function); !slices.Equal(asked, record) {
			test.Errorf("%s: asked directly the resolver answers %v, and its record is %v", path, asked, record)
		}
		if judged := return_nil_ness.Judged(kept.resolver, kept.home, function); !slices.Equal(judged, record) {
			test.Errorf("%s: the kind's own verdict is %v, and the resolver's record is %v", path, judged, record)
		}
	}
}
