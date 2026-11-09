package function_purity_test

import (
	"fmt"
	"go/types"
	"reflect"
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
)

// fixedStore is a store of answers for one package, by its path, which reloaded nothing.
type fixedStore struct {
	path    string
	answers *fact_resolver.Answers
}

func (store fixedStore) Answers(packageTypes *types.Package) *fact_resolver.Answers {
	if packageTypes.Path() != store.path {
		return nil
	}
	return store.answers
}

func (fixedStore) Reloaded(*types.Package) bool {
	return false
}

// storeProbe is what asking middle's reader of the purity of bottom.Pure gave, through a resolver over store, with
// bottom restored when restored is set: the answer, or what the question panicked with, and the resolver's counts.
type storeProbe struct {
	pure     bool
	panicked string
	counts   *fact_resolver.Counts
	hits     int64
}

// probeThroughStore asks middle's reader of the purity of bottom.Pure, through a resolver over store, with bottom
// restored when restored is set.
func probeThroughStore(test *testing.T, store fact_resolver.Store, restored bool) storeProbe {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"demand/hops/middle"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	var probed storeProbe
	probe := &analysis.Analyzer{
		Name:       "store_probe",
		Doc:        "asks the purity of bottom.Pure",
		Requires:   []*analysis.Analyzer{function_purity.Analyzer},
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			asked, isAsked := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
			if !isAsked {
				return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
			}
			pure, isPure := pass.Pkg.Imports()[0].Scope().Lookup("Pure").(*types.Func)
			if !isPure {
				return nil, fmt.Errorf("middle's import declares no function Pure")
			}
			probed.panicked = panicOf(func() { probed.pure = asked.IsPure(pure) })
			return "", nil
		},
	}
	resolver := fact_resolver.New(store)
	options := check.DispatchOptions()
	options.Resolver = resolver
	if restored {
		options.Restored = func(restoring *packages.Package) bool { return restoring.PkgPath == "demand/hops/bottom" }
		options.Restore = func(*checker.Action) error { return nil }
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
	probed.counts = resolver.Counts(function_purity.Kind)
	probed.hits = resolver.StoreHits()
	return probed
}

// A stored answer is read without a build: the store holds bottom.Pure not pure, though its body is pure, and the
// reader gets the store's answer, with nothing judged. A resolver that ignored the store would judge bottom.Pure, and
// answer pure.
func TestAStoredAnswerIsReadWithoutABuild(test *testing.T) {
	test.Parallel()
	store := fixedStore{path: "demand/hops/bottom", answers: &fact_resolver.Answers{
		Values:   map[string]map[objectpath.Path][]byte{function_purity.Kind.Name: {"Pure": []byte("false")}},
		Complete: map[string]bool{},
	}}
	probed := probeThroughStore(test, store, false)
	if probed.panicked != "" {
		test.Fatalf("asking bottom.Pure panicked: %s", probed.panicked)
	}
	if probed.pure || probed.counts.Computed.Load() != 0 || probed.hits != 1 {
		test.Fatalf("want the store's answer, not pure, with nothing judged and one store hit, got pure %t, %d "+
			"judged, %d store hits", probed.pure, probed.counts.Computed.Load(), probed.hits)
	}
	unstored := probeThroughStore(test, fixedStore{path: "demand/hops/bottom"}, false)
	if !unstored.pure || unstored.counts.Computed.Load() == 0 {
		test.Fatalf("want bottom.Pure judged pure with no store answer, got pure %t, %d judged", unstored.pure,
			unstored.counts.Computed.Load())
	}
}

// A package the store holds computed whole answers a function it holds no answer of as no fact, though the package is
// restored and cannot be built: completeness is what makes the absence an answer. The same store without the complete
// mark answers nothing there, and the question has the run load bottom from source.
func TestACompleteEntryAnswersAnAbsentFunctionAsNoFact(test *testing.T) {
	test.Parallel()
	complete := fixedStore{path: "demand/hops/bottom", answers: &fact_resolver.Answers{
		Values:   map[string]map[objectpath.Path][]byte{},
		Complete: map[string]bool{function_purity.Kind.Name: true},
	}}
	probed := probeThroughStore(test, complete, true)
	if probed.panicked != "" || probed.pure {
		test.Fatalf("want bottom.Pure answered not pure from the complete entry, got pure %t, panicked %q",
			probed.pure, probed.panicked)
	}
	partial := fixedStore{path: "demand/hops/bottom", answers: &fact_resolver.Answers{
		Values:   map[string]map[objectpath.Path][]byte{},
		Complete: map[string]bool{},
	}}
	reloaded := probeThroughStore(test, partial, true)
	const want = "fact_resolver: asked function_purity of demand/hops/bottom.Pure, of demand/hops/bottom, which the " +
		"run restored and no store answers, so the run loads it from source and runs again"
	if reloaded.panicked != want {
		test.Fatalf("want the question over a partial entry to reload bottom, panicking with %q, got %q", want,
			reloaded.panicked)
	}
}
