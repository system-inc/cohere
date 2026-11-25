package odd_slice_argument_test

import (
	"cmp"
	"go/types"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/project"
	"github.com/system-inc/cohere/internal/go_language/rules/odd_slice_argument"
	"github.com/system-inc/cohere/internal/needs"
)

// demandDifferentialVariable names the tree the demand differential runs over: root for cohere's own module, or a
// directory, whose every package it loads as cohere-go does. The standard library is its source folder, under
// `GOROOT` set to the toolchain it belongs to.
const demandDifferentialVariable = "COHERE_GO_IR_DEMAND_DIFFERENTIAL"

// The demand differential (#w7zmvat unit 2, #8d9r2rh): over the whole tree COHERE_GO_IR_DEMAND_DIFFERENTIAL names, and
// every package it imports, the push and the definition mark every function's parameters, SA5012 asks the resolver,
// and then the resolver is asked of every parameter of every package the push marked. Every answer must be the
// definition's, and every parameter the push marks must be marked; the push may leave unmarked only what its order
// alone left out, which is counted. It logs how many functions SA5012 had the resolver compute against the push's
// total, which is the saving. Unset, it skips.
func TestDemandAnswersAsTheDefinitionOverATree(test *testing.T) {
	test.Parallel()
	directory := needs.OptIn(
		test,
		demandDifferentialVariable,
		"a differential of the resolver's answers against the definition's over root or a directory",
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
	requireDemandAnswersAsTheDefinition(test, workspace.Root, loaded.Packages)
}

// requireDemandAnswersAsTheDefinition runs the push and the definition beside SA5012 over roots, as cohere-go runs
// it, then asks the resolver of every parameter of every package the push marked, as that package's own reader, and
// fails on the first answer the definition's marks contradict, on the first parameter the push marks that the
// resolver does not, or on any answer that came of replaying a package's walk.
func requireDemandAnswersAsTheDefinition(test *testing.T, described string, roots []*packages.Package) {
	test.Helper()
	var judged atomic.Int64
	push := odd_slice_argument.NewPushAnalyzer(&judged)
	options := check.DispatchOptions()
	options.Finished = func(*packages.Package) {
		// Setting Finished lets a root's syntax go, as cohere-go's run does; the resolver keeps its own.
	}
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("cohere-go's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{push, odd_slice_argument.DefinitionAnalyzer,
		odd_slice_argument.Analyzer}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", described, analyzeError)
	}
	counts := resolver.Counts(odd_slice_argument.Kind)
	askedByReaders := counts.Computed.Load()
	defined := map[*types.Package]map[*types.Var]bool{}
	for action := range graph.All() {
		if action.Analyzer == odd_slice_argument.DefinitionAnalyzer && action.Err == nil {
			defined[action.Package.Types] = ownMarks(action, odd_slice_argument.IsDefinitionMark)
		}
	}
	compared, pushPackages, pushMarks, departures := 0, 0, 0, 0
	for action := range graph.All() {
		if action.Analyzer != push || action.Err != nil {
			continue
		}
		pushPackages++
		pushed := ownMarks(action, odd_slice_argument.IsMark)
		pushMarks += len(pushed)
		definition, isDefined := defined[action.Package.Types]
		if !isDefined {
			test.Fatalf("the definition never marked %s, which the push marked", action.Package.ID)
		}
		home := resolver.Package(action.Package.Types)
		if home == nil {
			test.Fatalf("the resolver never learned %s, which the push marked", action.Package.ID)
		}
		asked := map[*types.Var]bool{}
		for _, parameter := range parametersOf(home) {
			compared++
			asked[parameter] = true
			answer := odd_slice_argument.Ask(resolver, home, parameter)
			if answer != definition[parameter] {
				test.Fatalf("on %s, parameter %s at %s: the resolver answers marked %t, the definition %t",
					action.Package.ID, parameter.Name(), action.Package.Fset.Position(parameter.Pos()), answer,
					definition[parameter])
			}
			if pushed[parameter] && !answer {
				test.Fatalf("on %s, parameter %s at %s: the push marks it and the resolver does not",
					action.Package.ID, parameter.Name(), action.Package.Fset.Position(parameter.Pos()))
			}
			if answer != pushed[parameter] {
				departures++
			}
		}
		for parameter := range definition {
			if !asked[parameter] {
				test.Fatalf("on %s, the definition marks %s at %s, which no parameter of the package's syntax names",
					action.Package.ID, parameter, action.Package.Fset.Position(parameter.Pos()))
			}
		}
	}
	if fallbacks := counts.Fallbacks.Load(); fallbacks != 0 {
		test.Fatalf("over %s the marks fell back %d times, want none", described, fallbacks)
	}
	if pushMarks == 0 {
		test.Fatalf("the push marked nothing over %s, so the comparison proves nothing", described)
	}
	test.Logf("%s: %d packages analyzed, the push ran on %d, walked %d functions and made %d marks; SA5012 had the "+
		"resolver compute %d (%.1f%%); every one of the %d parameters compared answers as the definition, and %d are "+
		"marked where the push's order left them unmarked", described, len(roots), pushPackages, judged.Load(),
		pushMarks, askedByReaders, 100*float64(askedByReaders)/float64(max(judged.Load(), 1)), compared, departures)
}

// ownMarks is every parameter of action's package its facts hold marked, by the fact isMark tells.
func ownMarks(action *checker.Action, isMark func(analysis.Fact) bool) map[*types.Var]bool {
	marked := map[*types.Var]bool{}
	for _, objectFact := range action.AllObjectFacts() {
		parameter, isParameter := objectFact.Object.(*types.Var)
		if isParameter && isMark(objectFact.Fact) && parameter.Pkg() == action.Package.Types {
			marked[parameter] = true
		}
	}
	return marked
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
