package return_nil_ness_test

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// Over the unbuilt fixture every answer is the push's, the closure's among them: Wrapped returns what a closure
// returns, and the closure, judged, would never return nil, but the push judges no closure, so Wrapped may. The replay
// takes its roots unbuilt, which holds declared functions alone, as the list it took built did, and reaches a closure
// only inside its parent's judgment, as before.
func TestTheUnbuiltFixtureAnswersAsThePush(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "unbuilt")
	requireVerdicts(test, probed, map[string]return_nil_ness.ValueNilness{
		"unbuilt.Outer 0":   {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil},
		"unbuilt.Inner 0":   {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.MaybeNil},
		"unbuilt.Wrapped 0": {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.MaybeNil},
		"unbuilt.After 0":   {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil},
	})
	if fallbacks := probed.counts.Fallbacks.Load(); fallbacks == 0 {
		test.Errorf("over unbuilt the resolver never replayed, so the replay's roots went untested")
	}
}

// One question of Inner, which replays the push's walk of the fixture from the top, must build only what it judges
// (#efs50c3): Outer and Inner. Size and Count, roots before Inner whose results can't be nil, are answered the
// defaults unbuilt, Count though Outer calls it, and Wrapped and After, roots past Inner, are never reached.
// No other analyzer runs, so no reader builds the package, and what is built there is what the question built.
func TestAReplayBuildsOnlyWhatItJudges(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"unbuilt"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	var built []string
	probe := &analysis.Analyzer{
		Name:       "replay_build_probe",
		Doc:        "asks the nilness of Inner once and keeps which of the package's functions are built",
		Requires:   []*analysis.Analyzer{return_nil_ness.Analyzer},
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			nilness, isNilness := pass.ResultOf[return_nil_ness.Analyzer].(*return_nil_ness.Result)
			if !isNilness {
				return nil, fmt.Errorf("return_nil_ness's result is a %T", pass.ResultOf[return_nil_ness.Analyzer])
			}
			inner, isInner := pass.Pkg.Scope().Lookup("Inner").(*types.Func)
			if !isInner {
				return nil, fmt.Errorf("unbuilt declares no function Inner")
			}
			nilness.Nilness(inner, 0)
			for _, function := range resolver.Package(pass.Pkg).IntermediateRepresentation().Functions {
				if function.Blocks != nil {
					built = append(built, function.Name())
				}
			}
			return "", nil
		},
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
		}
	}
	if fallbacks := resolver.Counts(return_nil_ness.Kind).Fallbacks.Load(); fallbacks != 1 {
		test.Fatalf("asking of Inner replayed %d times, want 1, so the replay's builds went untested", fallbacks)
	}
	slices.Sort(built)
	if want := []string{"Inner", "Outer"}; !slices.Equal(built, want) {
		test.Errorf("after one question of Inner the package has %v built, want %v", built, want)
	}
}
