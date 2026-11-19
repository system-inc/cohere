package return_nil_ness_test

import (
	"bytes"
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
// it, every nilness answer it recorded for the package, decoded, nil for no fact, by its function's objectpath, every
// nilness answer it holds about the package's functions (fact_resolver.Answered), decoded alike, by the function's
// name, with the function each is about, and the kind's counts.
type keptAnswers struct {
	resolver  *fact_resolver.Resolver
	home      *fact_resolver.Package
	recorded  map[string][]return_nil_ness.ValueNilness
	answered  map[string][]return_nil_ness.ValueNilness
	functions map[string]*types.Func
	counts    *fact_resolver.Counts
}

// askOnce runs a probe on the fixture package named fixture that asks the resolver the nilness of its function named
// asked, and of nothing else, and is what the resolver recorded and holds for the package. The run has a store, which
// answers nothing, since only a run with a store records its answers (fact_resolver.Recorded), as every engine run
// with the findings cache does; the fixtures import nothing, so the store is asked of no package restored.
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
		recorded: map[string][]return_nil_ness.ValueNilness{}, answered: map[string][]return_nil_ness.ValueNilness{},
		functions: map[string]*types.Func{}, counts: resolver.Counts(return_nil_ness.Kind)}
	if kept.home == nil {
		test.Fatalf("the resolver never learned %s, which the probe ran on", fixture)
	}
	records := resolver.Recorded(loaded.Roots[0].Types)[return_nil_ness.Kind.Name]
	for path, encoded := range records {
		var results []return_nil_ness.ValueNilness
		if decodeError := json.Unmarshal(encoded, &results); decodeError != nil {
			test.Fatalf("the answer recorded for %s, %s, does not decode: %v", path, encoded, decodeError)
		}
		kept.recorded[string(path)] = results
	}
	for _, answer := range resolver.Answered() {
		if answer.Kind != return_nil_ness.Kind.Name || answer.Object.Pkg() != kept.home.Types {
			continue
		}
		// An answer held and recorded both is the same bytes in each, no fact, a nil slice, among them.
		if path, pathError := objectpath.For(answer.Object); pathError == nil {
			if record, isRecorded := records[path]; isRecorded && !bytes.Equal(record, answer.Encoded) {
				test.Errorf("the answer held about %s is %s, and its record is %s", answer.Object.Name(),
					answer.Encoded, record)
			}
		}
		var results []return_nil_ness.ValueNilness
		if decodeError := json.Unmarshal(answer.Encoded, &results); decodeError != nil {
			test.Fatalf("the answer held about %s, %s, does not decode: %v", answer.Object.Name(), answer.Encoded,
				decodeError)
		}
		function, isFunction := answer.Object.(*types.Func)
		if !isFunction {
			test.Fatalf("the answer held about %s is about no function", answer.Object.Name())
		}
		kept.answered[function.Name()], kept.functions[function.Name()] = results, function
	}
	return kept
}

// requireKept fails unless the resolver holds an answer about exactly the functions of wantAnswered, each with its
// results, nil for an answer of no fact, and recorded exactly wantRecorded for the store, which is the function asked
// of and nothing a walk kept beside it, as a run records that keeps nothing.
func requireKept(
	test *testing.T,
	kept keptAnswers,
	wantAnswered map[string][]return_nil_ness.ValueNilness,
	wantRecorded map[string][]return_nil_ness.ValueNilness,
) {
	test.Helper()
	if !maps.EqualFunc(kept.answered, wantAnswered, slices.Equal[[]return_nil_ness.ValueNilness]) {
		test.Errorf("the resolver holds the answers %v, want %v", kept.answered, wantAnswered)
	}
	if !maps.EqualFunc(kept.recorded, wantRecorded, slices.Equal[[]return_nil_ness.ValueNilness]) {
		test.Errorf("the resolver recorded %v, want %v", kept.recorded, wantRecorded)
	}
}

// Asked of Caller alone, the resolver holds an answer of its own for each callee Caller's judgment judged, none of
// which was asked of (#kemy8sq): Fresh never nil, with its results; the unexported hidden, never nil too, which no
// objectpath names; Following, judged to no more than the defaults, as no fact; and Assembled, which has no body and
// takes the defaults unjudged, as no fact too. Caller's own answer, no fact, was built from those. What the run
// recorded for the store is Caller's answer alone, as a run that kept nothing records. No replay ran, so the answers
// are the ones the demand walk kept.
func TestEveryCalleeADemandWalkJudgesHasAnAnswerOfItsOwn(test *testing.T) {
	test.Parallel()
	kept := askOnce(test, "kept", "Caller")
	requireKept(test, kept, map[string][]return_nil_ness.ValueNilness{
		"Caller":    nil,
		"Fresh":     {{Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil}},
		"hidden":    {{Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil}},
		"Following": nil,
		"Assembled": nil,
	}, map[string][]return_nil_ness.ValueNilness{"Caller": nil})
	if fallbacks := kept.counts.Fallbacks.Load(); fallbacks != 0 {
		test.Errorf("asking of Caller replayed %d times, want 0, so the demand walk's answers went untested", fallbacks)
	}
}

// Asked of Inner alone, over the unbuilt fixture, the resolver replays the push's walk, which judges Outer on the way
// to Inner, and holds Outer's answer, never nil, beside Inner's, no fact, though no reader asked of Outer (#kemy8sq).
// Size and Count, which the replay answers the defaults and keeps no verdict of, have none. What the run recorded for
// the store is Inner's answer alone.
func TestEveryFunctionAReplayJudgesHasAnAnswerOfItsOwn(test *testing.T) {
	test.Parallel()
	kept := askOnce(test, "unbuilt", "Inner")
	requireKept(test, kept, map[string][]return_nil_ness.ValueNilness{
		"Inner": nil,
		"Outer": {{Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil}},
	}, map[string][]return_nil_ness.ValueNilness{"Inner": nil})
	if fallbacks := kept.counts.Fallbacks.Load(); fallbacks != 1 {
		test.Errorf("asking of Inner replayed %d times, want 1, so the replay's answers went untested", fallbacks)
	}
}

// The two walks keep the same functions (#kemy8sq): one declared without a body that has a result that may be nil,
// whose answer may change by platform, assembly on one and Go on another, and never one none of whose results may be
// nil, answered from its signature alone, the same in every build with the same types. Asked of Inner alone, the
// resolver replays the push, which meets Length and Assembled as roots before Outer: it holds Assembled's answer, the
// defaults of its signature, as no fact, and none of Length. Judged directly afterwards, Length goes through the
// demand walk, which holds none of it either. What the run recorded for the store is Inner's answer alone.
func TestTheWalksKeepABodilessFunctionAndNeverOneWithNoResultThatMayBeNil(test *testing.T) {
	test.Parallel()
	kept := askOnce(test, "assembled", "Inner")
	requireKept(test, kept, map[string][]return_nil_ness.ValueNilness{
		"Inner":     nil,
		"Outer":     {{Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil}},
		"Assembled": nil,
	}, map[string][]return_nil_ness.ValueNilness{"Inner": nil})
	if fallbacks := kept.counts.Fallbacks.Load(); fallbacks != 1 {
		test.Errorf("asking of Inner replayed %d times, want 1, so the replay's answers went untested", fallbacks)
	}
	length, isFunction := kept.home.Types.Scope().Lookup("Length").(*types.Func)
	if !isFunction {
		test.Fatal("assembled declares no function Length")
	}
	if judged := return_nil_ness.Judged(kept.resolver, kept.home, length); judged != nil {
		test.Errorf("Length's verdict is %v, want no fact", judged)
	}
	for _, answer := range kept.resolver.Answered() {
		if answer.Object == length {
			test.Errorf("the demand walk held Length's answer, %s, which no walk keeps", answer.Encoded)
		}
	}
}

// What a replay keeps of a cycle is what a later question answers (#kemy8sq). CycleOuter and CycleInner call each
// other, and asked of CycleInner alone the resolver replays the push, which begins at CycleOuter: CycleInner, reading
// its begun partner as the defaults, may return nil, where judged first it would return what CycleOuter does, never
// nil. Each verdict was handed to the resolver as the walk kept it, CycleInner's while CycleOuter was still being
// judged, and each is final: of every function the resolver holds an answer about, a question asked directly
// afterwards answers the same, and so does the kind's own verdict, which the answer was made from and which no later
// judgment revised.
func TestWhatAReplayKeepsOfACycleIsWhatALaterQuestionAnswers(test *testing.T) {
	test.Parallel()
	kept := askOnce(test, "differences", "CycleInner")
	if fallbacks := kept.counts.Fallbacks.Load(); fallbacks != 1 {
		test.Fatalf("asking of CycleInner replayed %d times, want 1, so the replay's answers went untested", fallbacks)
	}
	for name, want := range map[string][]return_nil_ness.ValueNilness{
		"CycleInner": nil,
		"CycleOuter": {{Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil}},
	} {
		if held, isHeld := kept.answered[name]; !isHeld || !slices.Equal(held, want) {
			test.Errorf("%s: the resolver holds %v (held: %t), want %v", name, held, isHeld, want)
		}
	}
	for name, held := range kept.answered {
		function := kept.functions[name]
		if asked, _ := return_nil_ness.Ask(kept.resolver, kept.home, function); !slices.Equal(asked, held) {
			test.Errorf("%s: asked directly the resolver answers %v, and the answer it held is %v", name, asked, held)
		}
		if judged := return_nil_ness.Judged(kept.resolver, kept.home, function); !slices.Equal(judged, held) {
			test.Errorf("%s: the kind's own verdict is %v, and the answer the resolver held is %v", name, judged, held)
		}
	}
}
