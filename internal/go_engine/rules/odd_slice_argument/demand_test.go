package odd_slice_argument_test

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

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
	"github.com/system-inc/cohere/internal/needs"
)

// demandDifferentialVariable names the tree the demand differential runs over: root for cohere's own module, or a
// directory, whose every package it loads as the engine does. The standard library is its source folder, under
// `GOROOT` set to the toolchain it belongs to.
const demandDifferentialVariable = "COHERE_GO_IR_DEMAND_DIFFERENTIAL"

// Two functions passing a slice to each other mark it in the push's order: visited as declared, Outer is marked. Asked
// of Inner first, as the probe asks in the order of names, a walk that read a begun function's marks as far as they had
// come would leave Outer unmarked; the resolver falls back to the push's walk instead and answers as the push does.
func TestACycleMarksInThePushsOrder(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "demand/cycle")
	requireVerdicts(test, probed, map[string]bool{
		"demand/cycle.Inner values": true,
		"demand/cycle.Outer values": true,
		"demand/cycle.Pairs pairs":  true,
	})
	if fallbacks := probed.counts.Fallbacks.Load(); fallbacks != 1 {
		test.Errorf("over demand/cycle the resolver fell back %d times, want 1, for its one cycle", fallbacks)
	}
}

// Over staticcheck's fixture and parity's packages, and everything they import, strings among them, every parameter
// of every function they reach answers as the push's result holds it.
func TestDemandAnswersAsThePushOnTheFixtures(test *testing.T) {
	test.Parallel()
	probed := probeFixtures(test, "CheckEvenSliceLength", "parity", "parity/pairs")
	requireVerdicts(test, probed, map[string]bool{"strings.NewReplacer oldnew": true})
}

// The demand differential (#w7zmvat unit 2): over the whole tree COHERE_GO_IR_DEMAND_DIFFERENTIAL names, and every
// package it imports, the push marks every function's parameters, SA5012 asks the resolver of other packages', and
// then the resolver is asked of every parameter of every package the push marked; every answer must be the push's,
// and every mark the push made must be one the resolver can be asked of. It logs how many functions SA5012 had the
// resolver compute against the push's total, which is the saving. Unset, it skips.
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

// requireDemandAnswersAsPush runs the push beside SA5012 over roots, as the engine runs it, then asks the resolver of
// every parameter of every package the push marked, as that package's own reader, and fails on the first answer the
// push's marks contradict, on a mark the resolver was never asked of, or on any verdict a fallback contradicted.
func requireDemandAnswersAsPush(test *testing.T, described string, roots []*packages.Package) {
	test.Helper()
	var judged atomic.Int64
	push := odd_slice_argument.NewPushAnalyzer(&judged)
	options := check.DispatchOptions()
	options.Finished = func(*packages.Package) {
		// Setting Finished lets a root's syntax go, as the engine's run does; the resolver keeps its own.
	}
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{push, odd_slice_argument.Analyzer}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", described, analyzeError)
	}
	counts := resolver.Counts(odd_slice_argument.Kind)
	askedByReaders, fallbacksByReaders := counts.Computed.Load(), counts.Fallbacks.Load()
	compared, pushPackages, pushMarks := 0, 0, 0
	for action := range graph.All() {
		if action.Analyzer != push || action.Err != nil {
			continue
		}
		pushPackages++
		pushed := map[*types.Var]bool{}
		for _, objectFact := range action.AllObjectFacts() {
			parameter, isParameter := objectFact.Object.(*types.Var)
			if isParameter && odd_slice_argument.IsMark(objectFact.Fact) && parameter.Pkg() == action.Package.Types {
				pushed[parameter] = true
			}
		}
		pushMarks += len(pushed)
		home := resolver.Package(action.Package.Types)
		if home == nil {
			test.Fatalf("the resolver never learned %s, which the push marked", action.Package.ID)
		}
		asked := map[*types.Var]bool{}
		for _, parameter := range parametersOf(home) {
			compared++
			asked[parameter] = true
			if answer := odd_slice_argument.Ask(resolver, home, parameter); answer != pushed[parameter] {
				test.Fatalf("on %s, parameter %s at %s: the resolver answers marked %t, the push %t",
					action.Package.ID, parameter.Name(), action.Package.Fset.Position(parameter.Pos()), answer,
					pushed[parameter])
			}
		}
		for parameter := range pushed {
			if !asked[parameter] {
				test.Fatalf("on %s, the push marked %s at %s, which no parameter of the package's syntax names",
					action.Package.ID, parameter, action.Package.Fset.Position(parameter.Pos()))
			}
		}
	}
	if contradicted := counts.Contradicted.Load(); contradicted != 0 {
		test.Fatalf("over %s a fallback contradicted %d marks the resolver had kept", described, contradicted)
	}
	if pushMarks == 0 {
		test.Fatalf("the push marked nothing over %s, so the comparison proves nothing", described)
	}
	test.Logf("%s: %d packages analyzed, the push ran on %d, walked %d functions and made %d marks; SA5012 had the "+
		"resolver compute %d (%.1f%%), with %d fallbacks (%d in all); every one of the %d parameters compared answers "+
		"as the push", described, len(roots), pushPackages, judged.Load(), pushMarks, askedByReaders,
		100*float64(askedByReaders)/float64(max(judged.Load(), 1)), fallbacksByReaders, counts.Fallbacks.Load(),
		compared)
}

// parametersOf is every named parameter of every function and function literal in home's syntax, receivers left out.
func parametersOf(home *fact_resolver.Package) []*types.Var {
	var parameters []*types.Var
	for _, defined := range home.Info().Defs {
		parameter, isParameter := defined.(*types.Var)
		if isParameter && !parameter.IsField() && parameter.Kind() == types.ParamVar {
			parameters = append(parameters, parameter)
		}
	}
	slices.SortFunc(parameters, func(left, right *types.Var) int { return cmp.Compare(left.Pos(), right.Pos()) })
	return parameters
}

// probed is what probing a fixture found: whether each parameter of each function is marked as the root that reaches
// it asks it, by the function's full name and the parameter's name, every place the resolver's answer and the push's
// result part, and the resolver's counts.
type probed struct {
	verdicts    map[string]bool
	differences []string
	counts      *fact_resolver.Counts
}

// probeFixtures runs a probe on each fixture package that asks the resolver of every parameter of every function in
// its types' reach, in the order of their names, and holds each answer to the push's result on the same package,
// failing the test where the two part.
func probeFixtures(test *testing.T, patterns ...string) probed {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	push := odd_slice_argument.NewPushAnalyzer(nil)
	probe := &analysis.Analyzer{
		Name:       "marks_probe",
		Doc:        "asks the resolver of every parameter of every function in the package's reach, beside the push",
		Requires:   []*analysis.Analyzer{odd_slice_argument.Analyzer, push},
		ResultType: reflect.TypeFor[probed](),
		Run: func(pass *analysis.Pass) (any, error) {
			resolver, resolverError := fact_resolver.Of(pass)
			if resolverError != nil {
				return nil, resolverError
			}
			reader := resolver.Package(pass.Pkg)
			if reader == nil {
				return nil, fmt.Errorf("the resolver never learned %s", pass.Pkg.Path())
			}
			found := probed{verdicts: map[string]bool{}}
			for _, function := range functionsInReach(pass.Pkg) {
				for parameter := range function.Signature().Params().Variables() {
					answer := odd_slice_argument.Ask(resolver, reader, parameter)
					pushed, isPushed := odd_slice_argument.PushMarked(pass, push, parameter)
					if !isPushed {
						return nil, fmt.Errorf("the push's result is a %T", pass.ResultOf[push])
					}
					if answer != pushed {
						found.differences = append(found.differences, fmt.Sprintf("in %s, %s's %s: the resolver "+
							"answers marked %t, the push %t", pass.Pkg.Path(), function.FullName(), parameter.Name(),
							answer, pushed))
					}
					found.verdicts[function.FullName()+" "+parameter.Name()] = answer
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
	all := probed{verdicts: map[string]bool{}, counts: resolver.Counts(odd_slice_argument.Kind)}
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

// requireVerdicts fails on each parameter of want whose mark, as the root reaching it asked it, is not want's.
func requireVerdicts(test *testing.T, found probed, want map[string]bool) {
	test.Helper()
	for name, marked := range want {
		verdict, asked := found.verdicts[name]
		if !asked {
			test.Errorf("%s was never asked", name)
			continue
		}
		if verdict != marked {
			test.Errorf("%s: the resolver answers marked %t, want %t", name, verdict, marked)
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
