package fact_resolver_test

import (
	"bytes"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// bottomStore is a store of one answers, bottom's, that reloaded nothing: nil answers, for a run that holds none.
type bottomStore struct {
	answers *fact_resolver.Answers
}

func (store bottomStore) Answers(packageTypes *types.Package) *fact_resolver.Answers {
	if packageTypes.Path() != "bodiless/bottom" {
		return nil
	}
	return store.answers
}

func (bottomStore) Reloaded(*types.Package) bool {
	return false
}

// bodilessQuestion is one kind's question of an object of bottom: the kind, the object, and the analyzer whose run on
// middle asks it.
type bodilessQuestion struct {
	kind     *fact_resolver.Kind
	object   func(bottom *types.Package) types.Object
	analyzer *analysis.Analyzer
}

// bottomAnswer is what a run that asked a question of bottom left: the answer recorded for bottom, as JSON, and
// whether there was one, the packages it had reloaded, whether middle was unsure, and how many functions the kind
// judged.
type bottomAnswer struct {
	answer   []byte
	answered bool
	reloads  []string
	unsure   bool
	computed int64
}

// bottomFunction is bottom's function named name, nil when it declares none.
func bottomFunction(bottom *types.Package, name string) *types.Func {
	function, _ := bottom.Scope().Lookup(name).(*types.Func)
	return function
}

// purityOf asks the purity of bottom's function named name.
func purityOf(name string) bodilessQuestion {
	return bodilessQuestion{
		kind:   function_purity.Kind,
		object: func(bottom *types.Package) types.Object { return bottomFunction(bottom, name) },
		analyzer: &analysis.Analyzer{
			Name:       "purity_probe",
			Doc:        "asks the purity of one of bottom's functions",
			Requires:   []*analysis.Analyzer{function_purity.Analyzer},
			ResultType: reflect.TypeFor[string](),
			Run: func(pass *analysis.Pass) (any, error) {
				asked, isAsked := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
				if !isAsked {
					return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
				}
				asked.IsPure(bottomFunction(pass.Pkg.Imports()[0], name))
				return "", nil
			},
		},
	}
}

// The questions of bottom's functions without a body, one of each kind computed on demand: the purity of Basic, the
// nilness of Pointer's result, and whether Slices's parameter is marked, which SA5012 asks of the call in middle.
var bodilessQuestions = map[string]bodilessQuestion{
	"function_purity": purityOf("Basic"),
	"return_nil_ness": {
		kind:   return_nil_ness.Kind,
		object: func(bottom *types.Package) types.Object { return bottomFunction(bottom, "Pointer") },
		analyzer: &analysis.Analyzer{
			Name:       "nilness_probe",
			Doc:        "asks the nilness of bottom.Pointer's result",
			Requires:   []*analysis.Analyzer{return_nil_ness.Analyzer},
			ResultType: reflect.TypeFor[string](),
			Run: func(pass *analysis.Pass) (any, error) {
				asked, isAsked := pass.ResultOf[return_nil_ness.Analyzer].(*return_nil_ness.Result)
				if !isAsked {
					return nil, fmt.Errorf("return_nil_ness's result is a %T", pass.ResultOf[return_nil_ness.Analyzer])
				}
				asked.Nilness(bottomFunction(pass.Pkg.Imports()[0], "Pointer"), 0)
				return "", nil
			},
		},
	},
	"odd_slice_argument": {
		kind: odd_slice_argument.Kind,
		object: func(bottom *types.Package) types.Object {
			return bottomFunction(bottom, "Slices").Signature().Params().At(0)
		},
		analyzer: odd_slice_argument.Analyzer,
	},
}

// askOfBottom runs question's analyzer on middle, loaded from source, with a store that holds no answer of bottom. With
// restored, bottom is restored, so the resolver never learns it, and the store holds what the load recorded of what
// bottom declares without a body when recordsBodies is set, and nothing of it otherwise.
func askOfBottom(test *testing.T, question bodilessQuestion, restored bool, recordsBodies bool) bottomAnswer {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"bodiless/middle"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	bottom := loaded.Roots[0].Imports["bodiless/bottom"]
	answers := &fact_resolver.Answers{Values: map[string]map[objectpath.Path][]byte{}, Complete: map[string]bool{}}
	if recordsBodies {
		paths, recorded := loaded.Bodiless.Of(bottom)
		answers.Bodiless, answers.BodiesRecorded = map[objectpath.Path]bool{}, recorded
		for _, path := range paths {
			answers.Bodiless[path] = true
		}
	}
	resolver := fact_resolver.New(bottomStore{answers: answers})
	options := check.DispatchOptions()
	options.Resolver = resolver
	if restored {
		options.Restored = func(restoring *packages.Package) bool { return restoring == bottom }
		options.Restore = func(*checker.Action) error { return nil }
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{question.analyzer}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("asking in %s: %v", action.Package.ID, action.Err)
		}
	}
	path, pathError := objectpath.For(question.object(bottom.Types))
	if pathError != nil {
		test.Fatalf("naming the object asked of: %v", pathError)
	}
	ran := bottomAnswer{unsure: resolver.Unsure(loaded.Roots[0].Types),
		computed: resolver.Counts(question.kind).Computed.Load()}
	ran.answer, ran.answered = resolver.Recorded(bottom.Types)[question.kind.Name][path]
	for _, reloaded := range resolver.Reloads() {
		ran.reloads = append(ran.reloads, reloaded.Path())
	}
	return ran
}

// Each kind's answer about a function declared without a body, from its types alone, is exactly what the kind
// computes when the package is loaded: the purity of bottom.Basic, the nilness of bottom.Pointer's result, the mark of
// bottom.Slices's parameter. Restored, with its bodies recorded, bottom is never reloaded, middle is sure, and the kind
// judged nothing (#b2t9xmf). A kind whose answer from types differed would be red here, as a store that held it would
// hand a restoring run another answer than a run that loaded bottom.
func TestABodilessAnswerIsTheOneComputedWhenThePackageIsLoaded(test *testing.T) {
	test.Parallel()
	for name, question := range bodilessQuestions {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			computed := askOfBottom(test, question, false, false)
			if !computed.answered || len(computed.reloads) > 0 {
				test.Fatalf("want the answer computed in bottom, loaded, got answered %t, reloads %v",
					computed.answered, computed.reloads)
			}
			fromTypes := askOfBottom(test, question, true, true)
			if !fromTypes.answered || len(fromTypes.reloads) > 0 || fromTypes.unsure || fromTypes.computed != 0 {
				test.Fatalf("want the answer from bottom's types, restored, with no reload, sure and nothing judged, "+
					"got answered %t, reloads %v, unsure %t, %d judged", fromTypes.answered, fromTypes.reloads,
					fromTypes.unsure, fromTypes.computed)
			}
			if !bytes.Equal(fromTypes.answer, computed.answer) {
				test.Fatalf("want the answer from types to be the computed one, %s, got %s", computed.answer,
					fromTypes.answer)
			}
		})
	}
}

// Only an object the store names as declared without a body is answered from types: bottom.Bodied, which has a body,
// reloads bottom though its bodies were recorded, and so does bottom.Basic, without one, when they were not, since
// export data cannot tell it from a function whose body the run never loaded. The load records Basic, Pointer and
// Slices, and Slices's parameter, and never Bodied.
func TestOnlyARecordedBodilessObjectIsAnsweredWithoutAReload(test *testing.T) {
	test.Parallel()
	for name, asked := range map[string]struct {
		question      bodilessQuestion
		recordsBodies bool
	}{
		"a function with a body": {question: purityOf("Bodied"), recordsBodies: true},
		"bodies not recorded":    {question: purityOf("Basic"), recordsBodies: false},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			ran := askOfBottom(test, asked.question, true, asked.recordsBodies)
			if ran.answered || !slices.Equal(ran.reloads, []string{"bodiless/bottom"}) || !ran.unsure {
				test.Fatalf("want bottom reloaded and middle unsure, got answered %t, reloads %v, unsure %t",
					ran.answered, ran.reloads, ran.unsure)
			}
		})
	}
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"bodiless/middle"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	bottom := loaded.Roots[0].Imports["bodiless/bottom"]
	paths, recorded := loaded.Bodiless.Of(bottom)
	slices.Sort(paths)
	parameter, pathError := objectpath.For(bottomFunction(bottom.Types, "Slices").Signature().Params().At(0))
	if pathError != nil {
		test.Fatalf("naming Slices's parameter: %v", pathError)
	}
	wanted := []objectpath.Path{"Basic", "Pointer", "Slices"}
	for _, function := range wanted {
		if !slices.Contains(paths, function) {
			test.Fatalf("want %s among the bodiless paths recorded, got %v", function, paths)
		}
	}
	if !recorded || !slices.Contains(paths, parameter) || slices.Contains(paths, "Bodied") {
		test.Fatalf("want Slices's parameter %s recorded and Bodied not, got recorded %t, %v", parameter, recorded,
			paths)
	}
}
