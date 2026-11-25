package fact_resolver_test

import (
	"go/types"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules/odd_slice_argument"
)

// reachedObjects is every object reached from the scope of each package in packages: its members, and through their
// types every method, interface method, struct field, parameter, result, receiver and type parameter, each once.
func reachedObjects(packages []*types.Package) []types.Object {
	var reached []types.Object
	seenObjects := map[types.Object]bool{}
	seenTypes := map[types.Type]bool{}
	var visitType func(visited types.Type)
	visitObject := func(object types.Object) {
		if object == nil || seenObjects[object] {
			return
		}
		seenObjects[object] = true
		reached = append(reached, object)
		visitType(object.Type())
	}
	visitTuple := func(tuple *types.Tuple) {
		for variable := range tuple.Variables() {
			visitObject(variable)
		}
	}
	visitTypeParameters := func(parameters *types.TypeParamList) {
		for parameter := range parameters.TypeParams() {
			visitObject(parameter.Obj())
		}
	}
	visitType = func(visited types.Type) {
		if visited == nil || seenTypes[visited] {
			return
		}
		seenTypes[visited] = true
		switch typed := visited.(type) {
		case *types.Named:
			visitObject(typed.Obj())
			visitTypeParameters(typed.TypeParams())
			for method := range typed.Methods() {
				visitObject(method)
			}
			visitType(typed.Underlying())
		case *types.Alias:
			visitObject(typed.Obj())
			visitType(typed.Rhs())
		case *types.Struct:
			for field := range typed.Fields() {
				visitObject(field)
			}
		case *types.Interface:
			for method := range typed.ExplicitMethods() {
				visitObject(method)
			}
			for embedded := range typed.EmbeddedTypes() {
				visitType(embedded)
			}
		case *types.Signature:
			if receiver := typed.Recv(); receiver != nil {
				visitObject(receiver)
			}
			visitTypeParameters(typed.TypeParams())
			visitTuple(typed.Params())
			visitTuple(typed.Results())
		case *types.Pointer:
			visitType(typed.Elem())
		case *types.Slice:
			visitType(typed.Elem())
		case *types.Array:
			visitType(typed.Elem())
		case *types.Map:
			visitType(typed.Key())
			visitType(typed.Elem())
		case *types.Chan:
			visitType(typed.Elem())
		case *types.TypeParam:
			visitObject(typed.Obj())
			visitType(typed.Constraint())
		case *types.Union:
			for term := range typed.Terms() {
				visitType(term.Type())
			}
		}
	}
	for _, scoped := range packages {
		for _, name := range scoped.Scope().Names() {
			visitObject(scoped.Scope().Lookup(name))
		}
	}
	return reached
}

// objectsAskers is how many goroutines ask the resolver's encoders at once, each every object reached, in its own
// rotation, so several ask the same package's encoder while it builds its index.
const objectsAskers = 8

// The resolver's encoders name every object exactly as objectpath.For does, the same path or the same lack of one,
// over every object reached from the scopes of the bodiless fixture and of go/ast, a package of named types, methods,
// interfaces, struct fields and generic functions: a path is an answer's key in the store, so one the encoders gave
// otherwise would key the same answer apart in two runs. The resolver first runs SA5012 on middle with bottom
// restored and not in its store, so the question of bottom.Slices's parameter is a miss: it names the parameter, of a
// package the resolver never learned, and reloads bottom. Then several goroutines ask every object at once, each twice,
// since an encoder searches its package the first time and answers from its index after.
func TestObjectsAskedAtOnceAreNamedAsObjectpathNamesThem(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"bodiless/middle", "go/ast"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	middle := loaded.Roots[0]
	if middle.PkgPath != "bodiless/middle" {
		middle = loaded.Roots[1]
	}
	bottom := middle.Imports["bodiless/bottom"]
	resolver := fact_resolver.New(bottomStore{answers: &fact_resolver.Answers{
		Values: map[string]map[objectpath.Path][]byte{}, Complete: map[string]bool{},
	}})
	options := check.DispatchOptions()
	options.Resolver = resolver
	options.Restored = func(restoring *packages.Package) bool { return restoring == bottom }
	options.Restore = func(*checker.Action) error { return nil }
	if _, analyzeError := checker.Analyze([]*analysis.Analyzer{odd_slice_argument.Analyzer},
		[]*packages.Package{middle}, options); analyzeError != nil {
		test.Fatalf("analyzing middle: %v", analyzeError)
	}
	reloads := resolver.Reloads()
	if resolver.Package(bottom.Types) != nil || len(reloads) != 1 || reloads[0] != bottom.Types {
		test.Fatalf("want bottom missed, never learned and reloaded, got learned %t, reloads %v",
			resolver.Package(bottom.Types) != nil, reloads)
	}
	var packages []*types.Package
	for _, root := range loaded.Roots {
		packages = append(packages, root.Types)
		for _, imported := range root.Imports {
			packages = append(packages, imported.Types)
		}
	}
	reached := reachedObjects(packages)
	named := 0
	ofBottom := 0
	for _, object := range reached {
		if _, pathError := objectpath.For(object); pathError == nil {
			named++
			if object.Pkg() == bottom.Types {
				ofBottom++
			}
		}
	}
	// Floors, so a walk that reached too little to test anything fails rather than passing on nothing.
	if named < 1000 || ofBottom < 5 {
		test.Fatalf("want at least 1000 objects with a path among the %d reached, 5 of them bottom's, got %d, %d",
			len(reached), named, ofBottom)
	}
	var askers sync.WaitGroup
	for asker := range objectsAskers {
		askers.Go(func() {
			for round := range 2 {
				for index := range reached {
					object := reached[(index+asker*len(reached)/objectsAskers)%len(reached)]
					wanted, wantedError := objectpath.For(object)
					got, gotError := resolver.ObjectPath(object)
					if got != wanted || (gotError == nil) != (wantedError == nil) {
						test.Errorf("asker %d, round %d, %s: want path %q, error %v, got %q, error %v", asker, round,
							object, wanted, wantedError, got, gotError)
						return
					}
				}
			}
		})
	}
	askers.Wait()
}
