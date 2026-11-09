package return_nil_ness_test

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// unansweringStore is a store that answers nothing and reloaded nothing, so a question of a restored package reloads.
type unansweringStore struct{}

func (unansweringStore) Answers(*types.Package) *fact_resolver.Answers {
	return nil
}

func (unansweringStore) Reloaded(*types.Package) bool {
	return false
}

// A question whose replay of the push's walk ends in a panic midway, here a reload of the restored package outside,
// leaves the walk with Outer and Inner marked seen and never judged. The next question must not read that walk: it
// replays again from the first root, reaches outside again, and reloads again. Kept, the walk would skip both as seen
// and answer Inner with no fact, silently, where the push holds a fact once outside is loaded from source.
func TestAQuestionAfterAnAbandonedReplayNeverReadsIt(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"replayed/inside"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	var panicked []string
	probe := &analysis.Analyzer{
		Name:       "replay_probe",
		Doc:        "asks the nilness of Inner twice and keeps what each question panicked with",
		Requires:   []*analysis.Analyzer{return_nil_ness.Analyzer},
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			nilness, isNilness := pass.ResultOf[return_nil_ness.Analyzer].(*return_nil_ness.Result)
			if !isNilness {
				return nil, fmt.Errorf("return_nil_ness's result is a %T", pass.ResultOf[return_nil_ness.Analyzer])
			}
			inner, isInner := pass.Pkg.Scope().Lookup("Inner").(*types.Func)
			if !isInner {
				return nil, fmt.Errorf("inside declares no function Inner")
			}
			for range 2 {
				panicked = append(panicked, panicOf(func() { nilness.Nilness(inner, 0) }))
			}
			return "", nil
		},
	}
	options := check.DispatchOptions()
	options.Resolver = fact_resolver.New(unansweringStore{})
	options.Restored = func(restoring *packages.Package) bool { return restoring.PkgPath == "replayed/outside" }
	options.Restore = func(*checker.Action) error { return nil }
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
		}
	}
	const reload = "fact_resolver: asked return_nil_ness of replayed/outside.Value, of replayed/outside, which the " +
		"run restored and no store answers"
	if len(panicked) != 2 {
		test.Fatalf("want two questions asked, got %d", len(panicked))
	}
	for index, question := range panicked {
		if !strings.HasPrefix(question, reload) {
			test.Errorf("question %d: want it to reload outside, got %q", index+1, question)
		}
	}
}

// panicOf is what run panicked with, as text, or empty when it returned.
func panicOf(run func()) (panicked string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicked = fmt.Sprint(recovered)
		}
	}()
	run()
	return ""
}
