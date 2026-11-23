package standing_facts_test

import (
	"go/ast"
	"slices"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// purity is the fact kind the tests' questions ask, function_purity's, whose package this import links into the test
// binary, so the kind is registered.
var purity = function_purity.Kind.Name

// fixture is a fixture package as one load made it, the build's side of a test: its first file as the load names it,
// and each of its function bodies' places by the function's name, a method's by its own.
type fixture struct {
	root   *packages.Package
	file   string
	places map[string]fact_reach.Place
}

// loadFacts loads the facts fixture and what it imports, from source, as the engine loads a package. Each run of the
// checker takes a load of its own, since a run lets go of what it read of the packages below its roots.
func loadFacts(test *testing.T) fixture {
	test.Helper()
	return loadFixture(test, "facts")
}

// loadFixture loads the fixture package at path, of one file, and what it imports, as loadFacts loads facts. Each
// presence fixture is a package of its own, since each imports what its row reads, whose closure every other test
// would load for nothing.
func loadFixture(test *testing.T, path string) fixture {
	test.Helper()
	found := loadVariant(test, path, path)
	if len(found.root.Syntax) != 1 {
		test.Fatalf("the %s fixture has %d files, want one", path, len(found.root.Syntax))
	}
	return found
}

// loadVariant loads the fixture package at path and what it imports, and is its root of the ID id: the package
// itself, or its test variant, `edges [edges.test]`, which holds its _test.go files too. file is the root's first
// file, the one identity names objects in, and the places are every file's.
func loadVariant(test *testing.T, path string, id string) fixture {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{path}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the %s fixture: %v", path, loadError)
	}
	rootAt := slices.IndexFunc(loaded.Roots, func(root *packages.Package) bool { return root.ID == id })
	if rootAt < 0 || len(loaded.Roots[rootAt].Syntax) == 0 {
		test.Fatalf("the load's roots are %v, want %s among them, of a file or more", loaded.Roots, id)
	}
	found := fixture{root: loaded.Roots[rootAt], places: map[string]fact_reach.Place{}}
	found.file = found.root.Fset.File(found.root.Syntax[0].FileStart).Name()
	for _, syntax := range found.root.Syntax {
		file := found.root.Fset.File(syntax.FileStart).Name()
		for _, declaration := range syntax.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
				found.places[function.Name.Name] = fact_reach.Place{File: file,
					Offset: found.root.Fset.Position(function.Pos()).Offset}
			}
		}
	}
	return found
}

// place is the place of the body of the fixture's function named name.
func (found fixture) place(test *testing.T, name string) fact_reach.Place {
	test.Helper()
	place, declared := found.places[name]
	if !declared {
		test.Fatalf("the %s fixture declares no function named %s", found.root.ID, name)
	}
	return place
}

// identity is how a host names the fixture's package-level object named name, in the file the build declares it in.
func (found fixture) identity(name string) build_reads.Identity {
	return build_reads.Identity{Kind: build_reads.IdentityObject, PackagePath: found.root.PkgPath,
		Path: objectpath.Path(name), File: found.file}
}

// method is how a host names the method named methodName of the fixture's type named typeName, in the file the build
// declares it in.
func (found fixture) method(typeName string, methodName string) build_reads.Identity {
	return build_reads.Identity{Kind: build_reads.IdentityMethod, PackagePath: found.root.PkgPath,
		Path: objectpath.Path(typeName), Method: methodName, File: found.file}
}

// imported is how a host names the package-level function named name of the package at packagePath, in a file of the
// host's own, which no build is held to for another package's function.
func imported(packagePath string, name string) build_reads.Identity {
	return build_reads.Identity{Kind: build_reads.IdentityObject, PackagePath: packagePath,
		Path: objectpath.Path(name), File: "/host/" + packagePath + ".go"}
}

// settle runs standing_facts on the fixture as the engine runs a rule's analyzer, the checker handing its pass host
// under rule.FactHost, or nothing for a nil host, and returns what the analyzer found and the run's resolver.
func settle(test *testing.T, found fixture, host fact_reach.Host) (*standing_facts.Result, *fact_resolver.Resolver) {
	test.Helper()
	options := check.DispatchOptions()
	options.FactHost = rule.FactHost
	options.FactHosts = func(packageId string) fact_reach.Host {
		if host == nil || packageId != found.root.ID {
			return nil
		}
		return host
	}
	action, resolver := analyze(test, found, standing_facts.Analyzer, options)
	settled, isSettled := action.Result.(*standing_facts.Result)
	if !isSettled {
		test.Fatalf("standing_facts's result is a %T", action.Result)
	}
	return settled, resolver
}

// analyze runs analyzer on the fixture under options, and returns its action there, ended without error, and the
// run's resolver, which holds every package of the fixture once the run has returned.
func analyze(
	test *testing.T,
	found fixture,
	analyzer *analysis.Analyzer,
	options *checker.Options,
) (*checker.Action, *fact_resolver.Resolver) {
	test.Helper()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{analyzer}, []*packages.Package{found.root}, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the %s fixture: %v", found.root.ID, analyzeError)
	}
	if len(graph.Roots) != 1 {
		test.Fatalf("running %s on the %s fixture left %d root actions, want one", analyzer.Name, found.root.ID,
			len(graph.Roots))
	}
	if graph.Roots[0].Err != nil {
		test.Fatalf("running %s on the %s fixture: %v", analyzer.Name, found.root.ID, graph.Roots[0].Err)
	}
	return graph.Roots[0], resolver
}

// function is the fixture's package-level function named name, as the build's types hold it.
func (found fixture) function(test *testing.T, name string) *types.Func {
	test.Helper()
	function, isFunction := found.root.Types.Scope().Lookup(name).(*types.Func)
	if !isFunction {
		test.Fatalf("the facts fixture declares no package-level function named %s", name)
	}
	return function
}

// buildsPurity is the build's own answer about the purity of object, as the fixture's readers are answered it once a
// run has returned, encoded as the kind encodes it, so no test guesses an answer's bytes.
func buildsPurity(
	test *testing.T,
	found fixture,
	resolver *fact_resolver.Resolver,
	object types.Object,
) []byte {
	test.Helper()
	reader := resolver.Package(found.root.Types)
	if reader == nil {
		test.Fatalf("the resolver never learned the facts fixture")
	}
	encoded, sure, registered := resolver.Asked(purity, reader, object)
	if !sure || !registered {
		test.Fatalf("asking the purity of %s: sure %t, registered %t, want both", object.Name(), sure, registered)
	}
	return encoded
}

// importedFunction is the function named name of the package at packagePath, which the fixture imports, directly or
// through an import, as the build's types hold it.
func (found fixture) importedFunction(test *testing.T, packagePath string, name string) *types.Func {
	test.Helper()
	reached := []*types.Package{found.root.Types}
	for next := 0; next < len(reached); next++ {
		if function, isFunction := reached[next].Scope().Lookup(name).(*types.Func); isFunction &&
			reached[next].Path() == packagePath {
			return function
		}
		reached = append(reached, reached[next].Imports()...)
	}
	test.Fatalf("the facts fixture reaches no function %s.%s", packagePath, name)
	return nil
}

// purityInAnotherRun is the build's answer about the purity of the function the fixture's package-level name names,
// asked in a load and a run of its own, for a host that must hold the build's answer before the build's run starts.
func purityInAnotherRun(test *testing.T, name string) []byte {
	test.Helper()
	found := loadFacts(test)
	_, resolver := analyze(test, found, function_purity.Analyzer, check.DispatchOptions())
	return buildsPurity(test, found, resolver, found.function(test, name))
}

// notPure is the kind's answer of no fact, encoded as the kind encodes it: a function not pure.
func notPure(test *testing.T) []byte {
	test.Helper()
	zero, registered := fact_resolver.ZeroAnswer(purity)
	if !registered {
		test.Fatalf("%s is not a registered kind", purity)
	}
	return zero
}
