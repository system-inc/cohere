package function_purity_test

import (
	"cmp"
	"fmt"
	"go/types"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules/function_purity"
)

// A call through an instance of the package's own pure generic function is the one place the push's answer depends on
// the order it walks the package in: package after declares the caller first and the push holds it not pure, package
// before declares it second and the push holds it pure. Asked on demand, the instance answers as its declared function
// does, so the caller is pure in both (#zva1zrc), and after's is the one place the push departs.
func TestAnInstanceAnswersAsItsDeclaredFunctionInEitherOrder(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "demand/order/after", "demand/order/before")
	requireVerdicts(test, probed, map[string]bool{
		"demand/order/after.Caller":  true,
		"demand/order/after.Zero":    true,
		"demand/order/before.Caller": true,
		"demand/order/before.Zero":   true,
	})
	requireDepartures(test, probed, []string{"demand/order/after.Caller"})
}

// A function's purity never turns on what else its package declares (#zva1zrc). Packages first and second are one
// package's two variants, differing only in an unrelated function and in which file comes first, and each calls the
// package's own pure functions through an instance of a generic function and through a method wrapper. The push holds
// both callers not pure in first, where it meets the callers first, and pure in second; asked on demand, every
// function answers the same in both.
func TestPurityNeverTurnsOnWhatElseThePackageDeclares(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "demand/declares/first", "demand/declares/second")
	want := map[string]bool{
		"InstanceCaller": true,
		"WrapperCaller":  true,
		"Zero":           true,
		"(Count).Double": true,
	}
	for name, pure := range want {
		for _, variant := range []string{"first", "second"} {
			full := "demand/declares/" + variant + "." + name
			if strings.HasPrefix(name, "(") {
				full = "(demand/declares/" + variant + "." + strings.TrimPrefix(name, "(")
			}
			requireVerdicts(test, probed, map[string]bool{full: pure})
		}
	}
	requireVerdicts(test, probed, map[string]bool{"demand/declares/first.Unrelated": true})
	// The push departs in first alone, which proves the variants differ where the push's order bites.
	requireDepartures(test, probed, []string{
		"demand/declares/first.InstanceCaller",
		"demand/declares/first.WrapperCaller",
	})
}

// A reader two imports above an exported function never holds its fact, which the checker drops on its way up, while
// a method's crosses every import: top holds middle.Middle and bottom's method pure, and bottom.Pure not.
func TestATwoHopFunctionReadsNotPure(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "demand/hops/top")
	requireVerdicts(test, probed, map[string]bool{
		"demand/hops/top.Top":               true,
		"demand/hops/middle.Middle":         true,
		"(demand/hops/bottom.Count).Double": true,
		"demand/hops/bottom.Pure":           false,
	})
}

// Over the purity fixture and everything it imports, strings, time, errors and fmt among them, every function the
// fixture reaches answers as the push's result holds it.
func TestDemandAnswersAsThePushOnTheFixture(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "purity")
	requireVerdicts(test, probed, map[string]bool{
		"purity.Add":        true,
		"strings.TrimSpace": true,
		"(time.Time).Unix":  true,
	})
}

// A question that reaches a package something restored in a run with no store of answers, which the resolver never
// learned and cannot build, fails loudly, naming the package, rather than answer not pure and drop the finding it would
// have made. Here bottom is restored, so middle.Middle, which calls bottom.Pure, cannot be judged.
func TestARestoredPackageFailsLoudly(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"demand/hops/top"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	probe := &analysis.Analyzer{
		Name:       "restored_probe",
		Doc:        "asks the purity of middle.Middle and returns what the question panicked with",
		Requires:   []*analysis.Analyzer{function_purity.Analyzer},
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			asked, isAsked := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
			if !isAsked {
				return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
			}
			middle, isMiddle := pass.Pkg.Imports()[0].Scope().Lookup("Middle").(*types.Func)
			if !isMiddle {
				return nil, fmt.Errorf("top's import declares no function Middle")
			}
			return panicOf(func() { asked.IsPure(middle) }), nil
		},
	}
	options := check.DispatchOptions()
	options.Restored = func(restored *packages.Package) bool { return restored.PkgPath == "demand/hops/bottom" }
	options.Restore = func(*checker.Action) error {
		// The restore gives no facts, and the resolver no store.
		return nil
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		panicked, isString := action.Result.(string)
		if action.Err != nil || !isString {
			test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
		}
		const want = "fact_resolver: asked about demand/hops/bottom.Pure of demand/hops/bottom, which it never " +
			"learned: a restored package, in a run with no store of answers"
		if panicked != want {
			test.Errorf("asking middle.Middle over a restored bottom panicked with %q, want %q", panicked, want)
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

// Purity never replays a package's walk: no answer of it depends on an order, so none falls back, the packages whose
// push departs included.
func TestPurityNeverFallsBack(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "demand/order/after", "demand/order/before", "demand/declares/first",
		"demand/declares/second", "demand/hops/top")
	if fallbacks := probed.counts.Fallbacks.Load(); fallbacks != 0 {
		test.Errorf("purity fell back %d times, want none", fallbacks)
	}
}

// probed is what probing a fixture found: each function's verdict as the root that reaches it asks it, by full name,
// every place the resolver's answer and the definition's part, or the push holds pure what the resolver does not, the
// functions the resolver holds pure where the push's order held them not, and the resolver's counts.
type probed struct {
	verdicts    map[string]bool
	differences []string
	departures  map[string]bool
	counts      *fact_resolver.Counts
}

// probeFixtures runs a probe on each fixture package that asks the resolver of every function in its types' reach and
// holds each answer to the definition's result on the same package, failing the test where the two part or where the
// push holds pure what the resolver does not.
func probeFixtures(test *testing.T, patterns ...string) probed {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	push := function_purity.NewPushAnalyzer()
	probe := &analysis.Analyzer{
		Name:       "purity_probe",
		Doc:        "asks the resolver of every function in the package's reach, beside the push's result",
		Requires:   []*analysis.Analyzer{function_purity.Analyzer, push, function_purity.DefinitionAnalyzer},
		ResultType: reflect.TypeFor[probed](),
		Run: func(pass *analysis.Pass) (any, error) {
			asked, isAsked := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
			if !isAsked {
				return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
			}
			found := probed{verdicts: map[string]bool{}, departures: map[string]bool{}}
			for _, function := range functionsInReach(pass.Pkg) {
				answer := asked.IsPure(function)
				pushed, isPushed := function_purity.PushHoldsPure(pass, push, function)
				if !isPushed {
					return nil, fmt.Errorf("the push's result is a %T", pass.ResultOf[push])
				}
				defined, isDefined := function_purity.DefinitionHoldsPure(pass, function)
				if !isDefined {
					return nil, fmt.Errorf("the definition's result is a %T",
						pass.ResultOf[function_purity.DefinitionAnalyzer])
				}
				if answer != defined {
					found.differences = append(found.differences, fmt.Sprintf("in %s, %s: the resolver answers "+
						"pure %t, the definition %t", pass.Pkg.Path(), function.FullName(), answer, defined))
				}
				if pushed && !answer {
					found.differences = append(found.differences, fmt.Sprintf("in %s, %s: the push holds it pure "+
						"and the resolver does not", pass.Pkg.Path(), function.FullName()))
				}
				if answer != pushed {
					found.departures[function.FullName()] = true
				}
				found.verdicts[function.FullName()] = answer
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
	all := probed{
		verdicts:   map[string]bool{},
		departures: map[string]bool{},
		counts:     resolver.Counts(function_purity.Kind),
	}
	for _, action := range graph.Roots {
		found, isFound := action.Result.(probed)
		if action.Err != nil || !isFound {
			test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
		}
		for name, verdict := range found.verdicts {
			all.verdicts[name] = verdict
		}
		for name := range found.departures {
			all.departures[name] = true
		}
		all.differences = append(all.differences, found.differences...)
	}
	for _, difference := range all.differences {
		test.Error(difference)
	}
	return all
}

// requireVerdicts fails on each function of want whose verdict, as the root reaching it asked it, is not want's.
func requireVerdicts(test *testing.T, found probed, want map[string]bool) {
	test.Helper()
	for name, pure := range want {
		verdict, asked := found.verdicts[name]
		if !asked {
			test.Errorf("%s was never asked", name)
			continue
		}
		if verdict != pure {
			test.Errorf("%s: the resolver answers pure %t, want %t", name, verdict, pure)
		}
	}
}

// requireDepartures fails unless the functions the resolver holds pure where the push's order held them not are want,
// by full name, exactly.
func requireDepartures(test *testing.T, found probed, want []string) {
	test.Helper()
	departed := slices.Sorted(maps.Keys(found.departures))
	if !slices.Equal(departed, slices.Sorted(slices.Values(want))) {
		test.Errorf("the push departs on %q, want %q", departed, want)
	}
}

// functionsInReach is every function and method of root and of every package below it, by full name.
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
