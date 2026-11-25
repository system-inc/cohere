package fact_resolver_test

import (
	"bytes"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules/function_purity"
	"github.com/system-inc/cohere/internal/go_language/rules/odd_slice_argument"
)

// askedQuestion is one question a pass on middle asks through the resolver's Asked, as middle: the kind's name, and
// the object, found from middle's types.
type askedQuestion struct {
	kindName string
	object   func(middle *types.Package) types.Object
}

// askedAnswer is what Asked answered one question.
type askedAnswer struct {
	encoded    []byte
	sure       bool
	registered bool
}

// middleRun is one run on middle, loaded from source: its resolver, middle's and bottom's types, and what the pass was
// answered, in the order it asked.
type middleRun struct {
	resolver *fact_resolver.Resolver
	middle   *types.Package
	bottom   *types.Package
	asked    []askedAnswer
}

// computedCount is a hand on a run's answers that answers nothing and counts each answer the run computed, by its
// kind's name and its object's.
type computedCount struct {
	lock     sync.Mutex
	computed map[string]int
}

func (*computedCount) Answer(string, types.Object) ([]byte, bool) {
	return nil, false
}

func (count *computedCount) Computed(kindName string, object types.Object, _ []byte) {
	count.lock.Lock()
	defer count.lock.Unlock()
	count.computed[kindName+" "+object.Name()]++
}

// The purity of bottom.Bodied, which bottom computes from its body, pure.
var bodiedPurityAsked = askedQuestion{
	kindName: function_purity.Kind.Name,
	object:   func(middle *types.Package) types.Object { return bottomFunction(middle.Imports()[0], "Bodied") },
}

// Whether the parameter of middle.Middle is marked, which middle computes by asking of the parameter of
// bottom.Slices, which Middle passes it on to.
var middleMarkAsked = askedQuestion{
	kindName: odd_slice_argument.Kind.Name,
	object: func(middle *types.Package) types.Object {
		function, _ := middle.Scope().Lookup("Middle").(*types.Func)
		return function.Signature().Params().At(0)
	},
}

// runOnMiddle runs, on middle loaded from source, a pass that asks questions through Asked with middle as the reader,
// and nothing when there are none, with a store that holds no answer and, when hand is not nil, hand on the run's
// answers. It requires ctrlflow alone, which the resolver learns a package from. With restored, bottom is restored, so
// the resolver never learns it.
func runOnMiddle(test *testing.T, restored bool, hand fact_resolver.Held, questions ...askedQuestion) middleRun {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"bodiless/middle"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	bottom := loaded.Roots[0].Imports["bodiless/bottom"]
	resolver := fact_resolver.New(bottomStore{})
	if hand != nil {
		resolver.HoldAnswers(hand)
	}
	options := check.DispatchOptions()
	options.Resolver = resolver
	if restored {
		options.Restored = func(restoring *packages.Package) bool { return restoring == bottom }
		options.Restore = func(*checker.Action) error { return nil }
	}
	asking := &analysis.Analyzer{
		Name:       "asked_probe",
		Doc:        "asks its questions through the resolver's Asked, with its package as the reader",
		Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
		ResultType: reflect.TypeFor[[]askedAnswer](),
		Run: func(pass *analysis.Pass) (any, error) {
			passResolver, resolverError := fact_resolver.Of(pass)
			if resolverError != nil {
				return nil, resolverError
			}
			reader := passResolver.Package(pass.Pkg)
			if reader == nil {
				return nil, fmt.Errorf("the resolver never learned %s", pass.Pkg.Path())
			}
			answers := []askedAnswer{}
			for _, question := range questions {
				var answer askedAnswer
				answer.encoded, answer.sure, answer.registered = passResolver.Asked(question.kindName, reader,
					question.object(pass.Pkg))
				answers = append(answers, answer)
			}
			return answers, nil
		},
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{asking}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	var root *checker.Action
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("asking in %s: %v", action.Package.ID, action.Err)
		}
		root = action
	}
	asked, isAnswers := root.Result.([]askedAnswer)
	if !isAnswers || len(asked) != len(questions) {
		test.Fatalf("want the pass's %d answers, got %v, a %T", len(questions), root.Result, root.Result)
	}
	return middleRun{resolver: resolver, middle: loaded.Roots[0].Types, bottom: bottom.Types, asked: asked}
}

// reloadPaths is the import path of every package run's resolver has the run reload, sorted.
func reloadPaths(run middleRun) []string {
	paths := []string{}
	for _, reloaded := range run.resolver.Reloads() {
		paths = append(paths, reloaded.Path())
	}
	slices.Sort(paths)
	return paths
}

// A question a pass asks through Asked is answered what Probe answers of the same kind and object after the run,
// bottom.Bodied pure, never the kind's answer of no fact, and it is sure. Asked twice and probed once, the answer is
// computed once. It is an ordinary question of its reader: the answer, another package's, is among middle's reads,
// which a question asked with the object's own package as the reader would never leave there.
func TestAnAnswerAskedDuringARunIsTheProbesComputedOnceAndReadByItsReader(test *testing.T) {
	test.Parallel()
	counting := &computedCount{computed: map[string]int{}}
	run := runOnMiddle(test, false, counting, bodiedPurityAsked, bodiedPurityAsked)
	probed, answered := run.resolver.Probe(function_purity.Kind.Name, run.bottom, "Bodied")
	if !answered || string(probed) != "true" {
		test.Fatalf("want the probe to answer Bodied pure, got %q, answered %t", probed, answered)
	}
	for _, asked := range run.asked {
		if !asked.registered || !asked.sure || !bytes.Equal(asked.encoded, probed) {
			test.Errorf("want the probe's answer %s, sure, of a kind registered, got %q, sure %t, registered %t",
				probed, asked.encoded, asked.sure, asked.registered)
		}
	}
	if computed := counting.computed[function_purity.Kind.Name+" Bodied"]; computed != 1 {
		test.Errorf("want Bodied's purity computed once for two questions and a probe, got %d of %v", computed,
			counting.computed)
	}
	reads, whole := run.resolver.Reads(run.middle)
	if !whole || len(reads) != 1 || reads[0].Kind != function_purity.Kind.Name || reads[0].Owner != run.bottom ||
		reads[0].Path != "Bodied" || !bytes.Equal(reads[0].Value, probed) {
		test.Errorf("want middle's one read to be Bodied's purity in bottom, %s, got whole %t, %v", probed, whole,
			reads)
	}
	if run.resolver.Unsure(run.middle) || len(reloadPaths(run)) > 0 {
		test.Errorf("want middle sure and nothing reloaded, got unsure %t, reloads %v",
			run.resolver.Unsure(run.middle), reloadPaths(run))
	}
}

// A kind's name nothing registered under is reported so, and nothing is asked: middle read nothing, and no kind
// computed an answer.
func TestAKindNeverRegisteredIsNotAsked(test *testing.T) {
	test.Parallel()
	counting := &computedCount{computed: map[string]int{}}
	run := runOnMiddle(test, false, counting, askedQuestion{
		kindName: "a_kind_never_registered",
		object:   bodiedPurityAsked.object,
	})
	if asked := run.asked[0]; asked.registered || asked.sure || asked.encoded != nil {
		test.Errorf("want no answer, not sure, of a kind not registered, got %q, sure %t, registered %t",
			asked.encoded, asked.sure, asked.registered)
	}
	if reads, _ := run.resolver.Reads(run.middle); len(reads) > 0 || len(counting.computed) > 0 {
		test.Errorf("want nothing asked, got middle's reads %v and answers computed %v", reads, counting.computed)
	}
}

// A question that must reach a package the run restored, of which the store holds no answer, is an ordinary one asked
// through Asked: bottom is among the reloads, middle is unsure, and the answer is reported not sure. That holds for a
// question of bottom's own object and for one of middle's object that middle computes from bottom's. A probe of the
// same kind and object, in a run whose pass asked nothing, answers that it could not, and reloads nothing.
func TestAQuestionAskedDuringARunReloadsARestoredPackageWhereAProbeCouldNotAnswer(test *testing.T) {
	test.Parallel()
	for name, question := range map[string]askedQuestion{
		"of the restored package's object":                 bodiedPurityAsked,
		"of the reader's object, computed from one of its": middleMarkAsked,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			run := runOnMiddle(test, true, nil, question)
			if asked := run.asked[0]; !asked.registered || asked.sure {
				test.Errorf("want the answer not sure, of a kind registered, got %q, sure %t, registered %t",
					asked.encoded, asked.sure, asked.registered)
			}
			if reloads := reloadPaths(run); !slices.Equal(reloads, []string{"bodiless/bottom"}) ||
				!run.resolver.Unsure(run.middle) {
				test.Errorf("want bottom reloaded and middle unsure, got reloads %v, unsure %t", reloads,
					run.resolver.Unsure(run.middle))
			}
			quiet := runOnMiddle(test, true, nil)
			object := question.object(quiet.middle)
			path, pathError := objectpath.For(object)
			if pathError != nil {
				test.Fatalf("naming the object asked of: %v", pathError)
			}
			probed, answered := quiet.resolver.Probe(question.kindName, object.Pkg(), path)
			if answered || len(reloadPaths(quiet)) > 0 {
				test.Errorf("want the probe to answer that it could not, reloading nothing, got %q, answered %t, "+
					"reloads %v", probed, answered, reloadPaths(quiet))
			}
		})
	}
}
