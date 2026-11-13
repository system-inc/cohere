package return_nil_ness_test

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
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
	"github.com/system-inc/cohere/internal/go_engine/rules/typed_nil_interface_comparison"
	"github.com/system-inc/cohere/internal/needs"
)

// demandDifferentialVariable names the tree the demand differential runs over: root for cohere's own module, or a
// directory, whose every package it loads as the engine does. The standard library is its source folder, under
// `GOROOT` set to the toolchain it belongs to.
const demandDifferentialVariable = "COHERE_GO_IR_DEMAND_DIFFERENTIAL"

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

// The demand differential (#w7zmvat unit 2): over the whole tree COHERE_GO_IR_DEMAND_DIFFERENTIAL names, and every
// package it imports, the push judges every function, SA4023, the reader of nilness, asks the resolver, and then the
// resolver is asked of every function the push judged; every answer must be the push's. It logs how many functions the
// reader had the resolver compute against the push's total, which is the saving. Unset, it skips.
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

// requireDemandAnswersAsPush runs the push beside SA4023 over roots, as the engine runs it, then asks the resolver of
// every function of every package the push judged, as that package's own reader, and fails on the first answer the
// push's facts contradict, or on any verdict a fallback contradicted.
func requireDemandAnswersAsPush(test *testing.T, described string, roots []*packages.Package) {
	test.Helper()
	var judged atomic.Int64
	push := return_nil_ness.NewPushAnalyzer(&judged)
	options := check.DispatchOptions()
	options.Finished = func(*packages.Package) {
		// Setting Finished lets a root's syntax go, as the engine's run does; the resolver keeps its own.
	}
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{push, typed_nil_interface_comparison.Analyzer}, roots,
		options)
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", described, analyzeError)
	}
	counts := resolver.Counts(return_nil_ness.Kind)
	askedByReaders, fallbacksByReaders := counts.Computed.Load(), counts.Fallbacks.Load()
	compared, pushPackages, pushFacts := 0, 0, 0
	for action := range graph.All() {
		if action.Analyzer != push || action.Err != nil {
			continue
		}
		pushPackages++
		pushed := map[*types.Func][]return_nil_ness.ValueNilness{}
		for _, objectFact := range action.AllObjectFacts() {
			function, isFunction := objectFact.Object.(*types.Func)
			returned, isReturned := objectFact.Fact.(*return_nil_ness.ReturnedNilness)
			if isFunction && isReturned && function.Pkg() == action.Package.Types {
				pushed[function] = returned.Results
			}
		}
		pushFacts += len(pushed)
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
			asked, holds := return_nil_ness.Ask(resolver, home, declared)
			fact, pushHolds := pushed[declared]
			if holds != pushHolds || holds && !slices.Equal(asked, fact) {
				test.Fatalf("on %s, %s (%s): the resolver answers %v (a fact: %t), the push %v (a fact: %t)",
					action.Package.ID, declared.FullName(), objectKey(action.Package, declared), asked, holds, fact,
					pushHolds)
			}
		}
	}
	if contradicted := counts.Contradicted.Load(); contradicted != 0 {
		test.Fatalf("over %s a fallback contradicted %d verdicts the resolver had kept", described, contradicted)
	}
	if pushFacts == 0 {
		test.Fatalf("the push held no fact over %s, so the comparison proves nothing", described)
	}
	test.Logf("%s: %d packages analyzed, the push ran on %d, judged %d functions and held %d facts; the reader had "+
		"the resolver compute %d (%.1f%%), with %d fallbacks (%d in all); every one of the %d functions compared "+
		"answers as the push", described, len(roots), pushPackages, judged.Load(), pushFacts, askedByReaders,
		100*float64(askedByReaders)/float64(max(judged.Load(), 1)), fallbacksByReaders, counts.Fallbacks.Load(),
		compared)
}

// objectKey is function's key for a failure: its objectpath, or its position for one with none.
func objectKey(owner *packages.Package, function *types.Func) string {
	path, pathError := objectpath.For(function)
	if pathError == nil {
		return "objectpath " + string(path)
	}
	return fmt.Sprintf("declared at %s", owner.Fset.Position(function.Pos()))
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
	push := return_nil_ness.NewPushAnalyzer(nil)
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
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
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
