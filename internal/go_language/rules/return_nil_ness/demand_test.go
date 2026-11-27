package return_nil_ness_test

import (
	"cmp"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules/return_nil_ness"
)

// A cycle of calls is where the push's answer hangs on its order: walked as declared, CycleOuter begins first and
// CycleInner, calling back into it, gets the defaults and may return nil, and the same for BetaOuter and BetaInner.
// Asked of an inner one first, as the probe asks in the order of names, a walk that answered a begun function with its
// defaults would judge the outer one whole and hold the inner one never nil; the resolver replays the push's walk
// instead and answers as the push does. The replay holds Alpha's verdict already, and must leave Fresh seen as judging
// Alpha would, or Gamma, reading Fresh through a method value, would read no fact. InnerFirst and OuterFirst, the phis
// read in either order, stay pinned beside them.
func TestACycleAnswersInThePushsOrder(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "differences")
	requireVerdicts(test, probed, map[string]return_nil_ness.ValueNilness{
		"differences.CycleInner 0": {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.MaybeNil},
		"differences.CycleOuter 0": {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil},
		"differences.InnerFirst 0": {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil},
		"differences.OuterFirst 0": {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil},
		"differences.BetaInner 0":  {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.MaybeNil},
		"differences.Gamma 0":      {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil},
	})
	// BetaInner, asked before CycleInner, replays the push past both cycles, so CycleInner is held by then.
	if fallbacks := probed.counts.Fallbacks.Load(); fallbacks != 1 {
		test.Errorf("over differences the resolver replayed %d times, want 1", fallbacks)
	}
}

// Over the nilness fixture and everything it imports, errors and fmt among them, every result of every function the
// fixture reaches answers as the push's result holds it.
func TestDemandAnswersAsThePushOnTheFixture(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "nilness")
	requireVerdicts(test, probed, map[string]return_nil_ness.ValueNilness{
		"nilness.Fresh 0": {Inner: return_nil_ness.MaybeNil, Outer: return_nil_ness.NeverNil},
		"errors.New 0":    {Inner: return_nil_ness.NeverNil, Outer: return_nil_ness.NeverNil},
	})
}

// probed is what probing a fixture found: the nilness of each result of each function as the root that reaches it asks
// it, by the function's full name and the result's index, every place the resolver's answer and the push's result
// part, and the resolver's counts.
type probed struct {
	verdicts    map[string]return_nil_ness.ValueNilness
	differences []string
	counts      *fact_resolver.Counts
}

// probeFixtures runs a probe on each fixture package that asks the resolver of every result of every function in its
// types' reach, in the order of their names, and holds each answer to the push's result on the same package, failing
// the test where the two part.
func probeFixtures(test *testing.T, patterns ...string) probed {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	push := return_nil_ness.NewPushAnalyzer()
	probe := &analysis.Analyzer{
		Name:       "nilness_probe",
		Doc:        "asks the resolver of every result of every function in the package's reach, beside the push's result",
		Requires:   []*analysis.Analyzer{return_nil_ness.Analyzer, push},
		ResultType: reflect.TypeFor[probed](),
		Run: func(pass *analysis.Pass) (any, error) {
			asked, isAsked := pass.ResultOf[return_nil_ness.Analyzer].(*return_nil_ness.Result)
			if !isAsked {
				return nil, fmt.Errorf("return_nil_ness's result is a %T", pass.ResultOf[return_nil_ness.Analyzer])
			}
			found := probed{verdicts: map[string]return_nil_ness.ValueNilness{}}
			for _, function := range functionsInReach(pass.Pkg) {
				for result := range function.Signature().Results().Len() {
					answer := asked.Nilness(function, result)
					pushed, isPushed := return_nil_ness.PushNilness(pass, push, function, result)
					if !isPushed {
						return nil, fmt.Errorf("the push's result is a %T", pass.ResultOf[push])
					}
					if answer != pushed {
						found.differences = append(found.differences, fmt.Sprintf("in %s, %s result %d: the "+
							"resolver answers %v, the push %v", pass.Pkg.Path(), function.FullName(), result, answer,
							pushed))
					}
					found.verdicts[fmt.Sprintf("%s %d", function.FullName(), result)] = answer
				}
			}
			return found, nil
		},
	}
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("cohere-go's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	all := probed{verdicts: map[string]return_nil_ness.ValueNilness{}, counts: resolver.Counts(return_nil_ness.Kind)}
	for _, action := range graph.Roots {
		found, isFound := action.Result.(probed)
		if action.Err != nil || !isFound {
			test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
		}
		for name, verdict := range found.verdicts {
			all.verdicts[name] = verdict
		}
		all.differences = append(all.differences, found.differences...)
	}
	for _, difference := range all.differences {
		test.Error(difference)
	}
	return all
}

// requireVerdicts fails on each result of want whose nilness, as the root reaching it asked it, is not want's.
func requireVerdicts(test *testing.T, found probed, want map[string]return_nil_ness.ValueNilness) {
	test.Helper()
	for name, nilness := range want {
		verdict, asked := found.verdicts[name]
		if !asked {
			test.Errorf("%s was never asked", name)
			continue
		}
		if verdict != nilness {
			test.Errorf("%s: the resolver answers %v, want %v", name, verdict, nilness)
		}
	}
}

// functionsInReach is every function and method of root and of every package below it, in the order of their full
// names.
func functionsInReach(root *types.Package) []*types.Func {
	var functions []*types.Func
	seen := map[*types.Package]bool{}
	var visit func(reached *types.Package)
	visit = func(reached *types.Package) {
		if seen[reached] {
			return
		}
		seen[reached] = true
		for _, imported := range reached.Imports() {
			visit(imported)
		}
		scope := reached.Scope()
		for _, name := range scope.Names() {
			switch object := scope.Lookup(name).(type) {
			case *types.Func:
				functions = append(functions, object)
			case *types.TypeName:
				if named, isNamed := object.Type().(*types.Named); isNamed {
					functions = append(functions, slices.Collect(named.Methods())...)
				}
			}
		}
	}
	visit(root)
	slices.SortFunc(functions, func(left, right *types.Func) int {
		return cmp.Compare(left.FullName(), right.FullName())
	})
	return functions
}
