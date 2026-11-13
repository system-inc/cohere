package function_purity_test

import (
	"cmp"
	"fmt"
	"go/types"
	"path/filepath"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rules/discarded_pure_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
	"github.com/system-inc/cohere/internal/go_engine/rules/self_assignment"
	"github.com/system-inc/cohere/internal/go_engine/rules/single_append_concatenation"
	"github.com/system-inc/cohere/internal/needs"
)

// demandDifferentialVariable names the tree the demand differential runs over: root for cohere's own module, or a
// directory, whose every package it loads as the engine does. The standard library is its source folder, under
// `GOROOT` set to the toolchain it belongs to.
const demandDifferentialVariable = "COHERE_GO_IR_DEMAND_DIFFERENTIAL"

// A call through an instance of the package's own pure generic function is the one place the push's answer depends on
// the order it walks the package in: package after declares the caller first and the push holds it not pure, package
// before declares it second and the push holds it pure. Asked on demand, each falls back to the push's walk and
// answers as the push does.
func TestAnInstanceAnswersInThePushsOrder(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "demand/order/after", "demand/order/before")
	requireVerdicts(test, probed, map[string]bool{
		"demand/order/after.Caller":  false,
		"demand/order/after.Zero":    true,
		"demand/order/before.Caller": true,
		"demand/order/before.Zero":   true,
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

// Each package an order-dependent answer was asked in is computed whole once, and counted; a package with no such
// answer never is.
func TestTheFallbackIsCounted(test *testing.T) {
	test.Parallel()
	for _, counted := range []struct {
		fixture   string
		fallbacks int64
	}{
		{"demand/order/after", 1},
		{"demand/order/before", 1},
		{"demand/hops/top", 0},
	} {
		probed := probeFixtures(test, counted.fixture)
		if fallbacks := probed.counts.Fallbacks.Load(); fallbacks != counted.fallbacks {
			test.Errorf("over %s the resolver fell back %d times, want %d", counted.fixture, fallbacks,
				counted.fallbacks)
		}
	}
}

// The demand differential (#w7zmvat unit 2): over the whole tree COHERE_GO_IR_DEMAND_DIFFERENTIAL names, and every
// package it imports, the push judges every function, the readers of purity ask the resolver, and then the resolver is
// asked of every function the push judged; every answer must be the push's. It logs how many functions the readers had
// the resolver compute against the push's total, which is the saving. Unset, it skips, since a tree takes minutes.
func TestDemandAnswersAsThePushOverATree(test *testing.T) {
	test.Parallel()
	directory := needs.OptIn(
		test,
		demandDifferentialVariable,
		"a differential of the resolver's answers against the push's over root or a directory",
	)
	if directory == "root" {
		// This package sits four directories below the module root.
		root, rootError := filepath.Abs(filepath.Join("..", "..", "..", ".."))
		if rootError != nil {
			test.Fatal(rootError)
		}
		directory = root
	}
	workspace, workspaceError := project.ReadWorkspace(directory)
	if workspaceError != nil {
		test.Fatalf("reading the modules of %s: %v", directory, workspaceError)
	}
	loaded, loadError := load.Packages(workspace.Root, []string{"./..."}, workspace.Environment(), nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", workspace.Root, loadError)
	}
	if len(loaded.Packages) == 0 {
		test.Fatalf("loading %s found no package, so the comparison would prove nothing", workspace.Root)
	}
	requireDemandAnswersAsPush(test, workspace.Root, loaded.Packages)
}

// requireDemandAnswersAsPush runs the push beside the three readers of purity over roots, as the engine runs them,
// then asks the resolver of every function of every package the push judged, as that package's own reader, and fails
// on the first answer the push's facts contradict, or on any verdict a fallback contradicted.
func requireDemandAnswersAsPush(test *testing.T, described string, roots []*packages.Package) {
	test.Helper()
	var judged atomic.Int64
	push := function_purity.NewPushAnalyzer(&judged)
	options := check.DispatchOptions()
	options.Finished = func(*packages.Package) {
		// Setting Finished lets a root's syntax go, as the engine's run does; the resolver keeps its own.
	}
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{push, discarded_pure_call.Analyzer,
		self_assignment.Analyzer, single_append_concatenation.Analyzer}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", described, analyzeError)
	}
	counts := resolver.Counts(function_purity.Kind)
	askedByReaders, fallbacksByReaders := counts.Computed.Load(), counts.Fallbacks.Load()
	compared, pushPackages := 0, 0
	for action := range graph.All() {
		if action.Analyzer != push || action.Err != nil {
			continue
		}
		pushPackages++
		pushed := map[*types.Func]bool{}
		for _, objectFact := range action.AllObjectFacts() {
			if function, isFunction := objectFact.Object.(*types.Func); isFunction &&
				function.Pkg() == action.Package.Types {
				pushed[function] = true
			}
		}
		home := resolver.Package(action.Package.Types)
		if home == nil {
			test.Fatalf("the resolver never learned %s, which the push judged", action.Package.ID)
		}
		for _, function := range home.SourceFunctions() {
			declared, isDeclared := function.Object().(*types.Func)
			if !isDeclared {
				continue
			}
			compared++
			if asked := function_purity.Ask(resolver, home, declared); asked != pushed[declared] {
				test.Fatalf("on %s, %s (%s): the resolver answers pure %t, the push %t", action.Package.ID,
					declared.FullName(), objectKey(action.Package, declared), asked, pushed[declared])
			}
		}
	}
	if contradicted := counts.Contradicted.Load(); contradicted != 0 {
		test.Fatalf("over %s a fallback contradicted %d verdicts the resolver had kept", described, contradicted)
	}
	if compared == 0 {
		test.Fatalf("the push judged no function over %s, so the comparison proves nothing", described)
	}
	test.Logf("%s: %d packages analyzed, the push ran on %d and judged %d functions; the readers had the resolver "+
		"compute %d (%.1f%%), with %d fallbacks; every one of the %d functions compared answers as the push",
		described, len(roots), pushPackages, judged.Load(), askedByReaders,
		100*float64(askedByReaders)/float64(max(judged.Load(), 1)), fallbacksByReaders, compared)
}

// objectKey is function's key for a failure: its objectpath, or its position for one with none.
func objectKey(owner *packages.Package, function *types.Func) string {
	path, pathError := objectpath.For(function)
	if pathError == nil {
		return "objectpath " + string(path)
	}
	return fmt.Sprintf("declared at %s", owner.Fset.Position(function.Pos()))
}

// probed is what probing a fixture found: each function's verdict as the root that reaches it asks it, by full name,
// every place the resolver's answer and the push's result part, and the resolver's counts.
type probed struct {
	verdicts    map[string]bool
	differences []string
	counts      *fact_resolver.Counts
}

// probeFixtures runs a probe on each fixture package that asks the resolver of every function in its types' reach and
// holds each answer to the push's result on the same package, failing the test where the two part.
func probeFixtures(test *testing.T, patterns ...string) probed {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	push := function_purity.NewPushAnalyzer(nil)
	probe := &analysis.Analyzer{
		Name:       "purity_probe",
		Doc:        "asks the resolver of every function in the package's reach, beside the push's result",
		Requires:   []*analysis.Analyzer{function_purity.Analyzer, push},
		ResultType: reflect.TypeFor[probed](),
		Run: func(pass *analysis.Pass) (any, error) {
			asked, isAsked := pass.ResultOf[function_purity.Analyzer].(*function_purity.Result)
			if !isAsked {
				return nil, fmt.Errorf("function_purity's result is a %T", pass.ResultOf[function_purity.Analyzer])
			}
			found := probed{verdicts: map[string]bool{}}
			for _, function := range functionsInReach(pass.Pkg) {
				answer := asked.IsPure(function)
				pushed, isPushed := function_purity.PushHoldsPure(pass, push, function)
				if !isPushed {
					return nil, fmt.Errorf("the push's result is a %T", pass.ResultOf[push])
				}
				if answer != pushed {
					found.differences = append(found.differences, fmt.Sprintf("in %s, %s: the resolver answers "+
						"pure %t, the push %t", pass.Pkg.Path(), function.FullName(), answer, pushed))
				}
				found.verdicts[function.FullName()] = answer
			}
			return found, nil
		},
	}
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	all := probed{verdicts: map[string]bool{}, counts: resolver.Counts(function_purity.Kind)}
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
