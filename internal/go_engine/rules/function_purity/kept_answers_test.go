package function_purity_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/types"
	"maps"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
)

// keptAnswers is what one question left in the resolver of a fixture package: the resolver, the package as it holds
// it, every purity answer it recorded for the package, by its function's objectpath, every purity answer it holds
// about the package's functions (fact_resolver.Answered), by the function's name, and the function each is about.
type keptAnswers struct {
	resolver  *fact_resolver.Resolver
	home      *fact_resolver.Package
	recorded  map[string]bool
	answered  map[string]bool
	functions map[string]*types.Func
}

// askOnce runs a probe on the fixture package named fixture that asks the resolver the purity of its function named
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
		Doc:        "asks the purity of one function of the package and of nothing else",
		Requires:   []*analysis.Analyzer{function_purity.Analyzer},
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			purity, isPurity := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
			if !isPurity {
				return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
			}
			function, isFunction := pass.Pkg.Scope().Lookup(asked).(*types.Func)
			if !isFunction {
				return nil, fmt.Errorf("%s declares no function %s", fixture, asked)
			}
			purity.IsPure(function)
			return "", nil
		},
	}
	// A store of no package's answers: its path is no package's.
	resolver := fact_resolver.New(fixedStore{})
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
	kept := keptAnswers{resolver: resolver, home: resolver.Package(loaded.Roots[0].Types), recorded: map[string]bool{},
		answered: map[string]bool{}, functions: map[string]*types.Func{}}
	if kept.home == nil {
		test.Fatalf("the resolver never learned %s, which the probe ran on", fixture)
	}
	records := resolver.Recorded(loaded.Roots[0].Types)[function_purity.Kind.Name]
	for path, encoded := range records {
		var pure bool
		if decodeError := json.Unmarshal(encoded, &pure); decodeError != nil {
			test.Fatalf("the answer recorded for %s, %s, does not decode: %v", path, encoded, decodeError)
		}
		kept.recorded[string(path)] = pure
	}
	for _, answer := range resolver.Answered() {
		if answer.Kind != function_purity.Kind || answer.Object.Pkg() != kept.home.Types {
			continue
		}
		// An answer held and recorded both is the same bytes in each.
		if path, pathError := objectpath.For(answer.Object); pathError == nil {
			if record, isRecorded := records[path]; isRecorded && !bytes.Equal(record, answer.Encoded) {
				test.Errorf("the answer held about %s is %s, and its record is %s", answer.Object.Name(),
					answer.Encoded, record)
			}
		}
		var pure bool
		if decodeError := json.Unmarshal(answer.Encoded, &pure); decodeError != nil {
			test.Fatalf("the answer held about %s, %s, does not decode: %v", answer.Object.Name(), answer.Encoded,
				decodeError)
		}
		function, isFunction := answer.Object.(*types.Func)
		if !isFunction {
			test.Fatalf("the answer held about %s is about no function", answer.Object.Name())
		}
		kept.answered[function.Name()], kept.functions[function.Name()] = pure, function
	}
	return kept
}

// requireFinal fails unless each answer the resolver holds is the one a question asked directly afterwards answers.
func (kept keptAnswers) requireFinal(test *testing.T) {
	test.Helper()
	for name, held := range kept.answered {
		if asked := function_purity.Ask(kept.resolver, kept.home, kept.functions[name]); asked != held {
			test.Errorf("%s: asked directly the resolver answers pure %t, and the answer it held is %t", name, asked,
				held)
		}
	}
}

// Asked of Caller alone, the resolver holds an answer of its own for each callee Caller's judgment judged, none of
// which was asked of (#z9fze0t): Fresh, pure; the unexported hidden, pure too, which no objectpath names; and Printed,
// not pure, where the judgment ended. After, called past Printed, was never judged and has none. Caller's own answer,
// not pure, was built from those. What the run recorded for the store is Caller's answer alone, as a run that kept
// nothing records, and each answer held is the one a later question answers.
func TestEveryCalleeAWalkJudgesHasAnAnswerOfItsOwn(test *testing.T) {
	test.Parallel()
	kept := askOnce(test, "kept", "Caller")
	wantAnswered := map[string]bool{"Caller": false, "Fresh": true, "hidden": true, "Printed": false}
	if !maps.Equal(kept.answered, wantAnswered) {
		test.Errorf("the resolver holds the answers %v, want %v", kept.answered, wantAnswered)
	}
	if wantRecorded := map[string]bool{"Caller": false}; !maps.Equal(kept.recorded, wantRecorded) {
		test.Errorf("the resolver recorded %v, want %v", kept.recorded, wantRecorded)
	}
	kept.requireFinal(test)
}

// What a walk keeps of a cycle is what a later question answers (#z9fze0t). Even and Odd call each other, and asked
// of Even alone the walk judges Odd inside Even's judgment: Odd, reading Even still being judged, is not pure, and
// its verdict is handed to the resolver while Even's judgment is still open. It is final all the same, a member of a
// cycle never being pure in any order: asked directly afterwards, each answers as the resolver held it.
func TestWhatAWalkKeepsOfACycleIsWhatALaterQuestionAnswers(test *testing.T) {
	test.Parallel()
	kept := askOnce(test, "purity", "Even")
	wantAnswered := map[string]bool{"Even": false, "Odd": false}
	if !maps.Equal(kept.answered, wantAnswered) {
		test.Errorf("the resolver holds the answers %v, want %v", kept.answered, wantAnswered)
	}
	if wantRecorded := map[string]bool{"Even": false}; !maps.Equal(kept.recorded, wantRecorded) {
		test.Errorf("the resolver recorded %v, want %v", kept.recorded, wantRecorded)
	}
	kept.requireFinal(test)
}
